package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/giocaizzi/agents/internal/cache"
	"github.com/giocaizzi/agents/internal/doctor"
	"github.com/giocaizzi/agents/internal/provider"
	"github.com/giocaizzi/agents/internal/term"
	usg "github.com/giocaizzi/agents/internal/usage"
)

// doctorBudget bounds one doctor run: the whole run (offline or live), and
// inside a live run each provider query and the identity resolution.
type doctorBudget struct {
	offline, live, perProvider, identity time.Duration
}

// doctorGrace is how long an overrun run waits for its worker after cancel.
const doctorGrace = 250 * time.Millisecond

// quietLines caps the loud lines of -q output (the apply hook).
const quietLines = 5

func defaultDoctorBudget() doctorBudget {
	return doctorBudget{
		offline:     2 * time.Second,
		live:        6 * time.Second,
		perProvider: 2 * time.Second,
		identity:    2 * time.Second,
	}
}

func (d Deps) runDoctor(ctx context.Context, args []string) int {
	store := newOSStore(d)
	return d.runDoctorWith(ctx, args, store, newFetcher(store.home))
}

// runDoctorWith is runDoctor with the credential store and the fetcher injected.
func (d Deps) runDoctorWith(ctx context.Context, args []string, store provider.Store, f usageFetcher) int {
	return d.runDoctorBudget(ctx, args, store, f, defaultDoctorBudget())
}

// doctorRun is what one doctor run wrote: it prints only if it finished in time.
type doctorRun struct {
	out, err bytes.Buffer
	code     int
}

// runDoctorBudget is runDoctorWith with the time budget injected.
func (d Deps) runDoctorBudget(ctx context.Context, args []string, store provider.Store, f usageFetcher, b doctorBudget) int {
	flags := newFlags("doctor")
	live := flags.Bool("live", false, "query every provider and rewrite the usage cache")
	quiet := flags.Bool("q", false, "print nothing when there is nothing to report")
	asJSON := flags.Bool("json", false, "print the findings as JSON")
	harnessList := flags.String("harness", "", "comma-separated harnesses")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(d.Stderr, "flag parse error: %v\n", err)
		return 2
	}
	budget := b.offline
	if *live {
		budget = b.live
	}

	// The worker owns its buffers and hands them over on done; after an
	// overrun nothing reads them. It stops with ctx (cancelled on overrun),
	// except in a file read that cannot block (readRegular), so it ends on
	// its own; a worker stuck in a call that ignores ctx dies with the process.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan *doctorRun, 1)
	go func() {
		done <- d.doctorWork(ctx, store, f, b, *live, *quiet, *asJSON, *harnessList)
	}()
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case r := <-done:
		_, _ = io.Copy(d.Stdout, &r.out)
		_, _ = io.Copy(d.Stderr, &r.err)
		return r.code
	case <-timer.C:
		cancel()
		// Give a worker that is writing the cache a moment to finish or bail out.
		grace := time.NewTimer(doctorGrace)
		defer grace.Stop()
		select {
		case <-done:
		case <-grace.C:
		}
		if !*quiet {
			fmt.Fprintf(d.Stderr, "doctor: timed out after %s\n", budget)
		}
		return 0
	}
}

// doctorWork loads the inputs, runs the checks and renders into a doctorRun.
func (d Deps) doctorWork(ctx context.Context, store provider.Store, f usageFetcher, b doctorBudget, live, quiet, asJSON bool, harnessList string) *doctorRun {
	r := &doctorRun{}
	fail := func(err error) *doctorRun {
		fmt.Fprintf(&r.err, "doctor: %v\n", err)
		r.code = 1
		return r
	}
	note := func(format string, a ...any) {
		if !quiet {
			fmt.Fprintf(&r.err, format, a...)
		}
	}

	cat, _, err := d.loadCatalog("", false)
	if err != nil {
		note("doctor: ignoring model catalog: %v\n", err)
	}
	pol, _, err := d.loadPolicy("")
	if err != nil {
		return fail(err)
	}
	dir, err := cache.Dir(d.Getenv)
	if err != nil {
		return fail(err)
	}
	path := filepath.Join(dir, usageCacheFile)
	now := f.now()

	cached, cacheErr := readUsageCache(path)
	if cacheErr != nil {
		note("doctor: ignoring usage cache: %v\n", cacheErr)
	}
	var (
		snaps []usg.Snapshot
		meta  string
	)
	haveUsage := !cached.WrittenAt.IsZero()
	switch {
	case live:
		var werr error
		snaps, werr = refreshUsage(ctx, store, f, path, cached, b.perProvider, b.identity)
		if werr != nil {
			note("doctor: usage cache not written: %v\n", werr)
		}
		haveUsage, meta = true, "(live)"
	case haveUsage:
		snaps = cached.Snapshots
		meta = "(cached " + usg.HumanDuration(now.Sub(cached.WrittenAt)) + " ago; -live to refresh)"
	default:
		meta = "(no usage cache; -live to refresh)"
	}
	clean := make([]usg.Snapshot, len(snaps))
	for i, s := range snaps {
		clean[i] = sanitized(s)
	}

	harnesses := splitHarnesses(harnessList)
	if len(harnesses) == 0 {
		harnesses = snapshotHarnesses(clean)
	}
	harnesses = orderHarnesses(harnesses)
	findings := doctor.Check(doctor.Input{
		Catalog:   cat,
		Settings:  doctor.Settings(*pol, harnesses),
		Harnesses: harnesses,
		Snapshots: clean,
		HaveUsage: haveUsage,
		Now:       now,
	})

	switch {
	case asJSON:
		if err := writeDoctorJSON(&r.out, findings); err != nil {
			return fail(err)
		}
	case quiet && !doctor.Loud(findings):
	default:
		limit := 0
		if quiet {
			limit = quietLines
		}
		renderDoctor(&r.out, term.NewStyle(d.Stdout, d.Getenv), findings, meta, limit)
	}
	return r
}

// writeDoctorJSON writes {"findings": [...]}, an empty list as [].
func writeDoctorJSON(w io.Writer, findings []doctor.Finding) error {
	if findings == nil {
		findings = []doctor.Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(struct {
		Findings []doctor.Finding `json:"findings"`
	}{findings}); err != nil {
		return fmt.Errorf("encode findings: %w", err)
	}
	return nil
}

// splitHarnesses splits a -harness value on commas and whitespace, dropping
// empties and duplicates.
func splitHarnesses(s string) []string {
	var out []string
	for _, h := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		if !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

// snapshotHarnesses is the union of the snapshots' harnesses.
func snapshotHarnesses(snaps []usg.Snapshot) []string {
	var out []string
	for _, s := range snaps {
		for _, h := range s.Harnesses {
			if h != "" && !slices.Contains(out, h) {
				out = append(out, h)
			}
		}
	}
	return out
}

// orderHarnesses sorts harnesses into display order: the known ones in
// harnessOrder, then the others alphabetically.
func orderHarnesses(hs []string) []string {
	known := []string{harnessClaude, harnessCopilot, harnessCodex, harnessAntigravity, harnessOpencode, harnessPi}
	rank := func(h string) int {
		if i := slices.Index(known, h); i >= 0 {
			return i
		}
		return len(known)
	}
	out := slices.Clone(hs)
	slices.SortStableFunc(out, func(a, b string) int {
		if c := rank(a) - rank(b); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return out
}
