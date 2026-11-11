package cli

import (
	"bytes"
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
	"github.com/giocaizzi/agents/internal/term"
	usg "github.com/giocaizzi/agents/internal/usage"
)

// doctorBudget bounds one doctor run.
const doctorBudget = 2 * time.Second

// quietLines caps the loud lines of -q output (the apply hook).
const quietLines = 5

func (d Deps) runDoctor(args []string) int {
	return d.runDoctorBudget(args, time.Now, doctorBudget)
}

// doctorRun is what one doctor run wrote: it prints only if it finished in time.
type doctorRun struct {
	out, err bytes.Buffer
	code     int
}

// runDoctorBudget is runDoctor with the clock and the time budget injected.
func (d Deps) runDoctorBudget(args []string, now func() time.Time, budget time.Duration) int {
	flags := newFlags("doctor")
	quiet := flags.Bool("q", false, "print nothing when there is nothing to report")
	asJSON := flags.Bool("json", false, "print the findings as JSON")
	harnessList := flags.String("harness", "", "comma-separated harnesses")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(d.Stderr, "flag parse error: %v\n", err)
		return 2
	}

	// The worker owns its buffers and hands them over on done; after an
	// overrun nothing reads them. It only reads files that cannot block
	// (readRegular), so it ends on its own.
	done := make(chan *doctorRun, 1)
	go func() {
		done <- d.doctorWork(now, *quiet, *asJSON, *harnessList)
	}()
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case r := <-done:
		_, _ = io.Copy(d.Stdout, &r.out)
		_, _ = io.Copy(d.Stderr, &r.err)
		return r.code
	case <-timer.C:
		if !*quiet {
			fmt.Fprintf(d.Stderr, "doctor: timed out after %s\n", budget)
		}
		return 0
	}
}

// doctorWork loads the inputs, runs the checks and renders into a doctorRun.
func (d Deps) doctorWork(now func() time.Time, quiet, asJSON bool, harnessList string) *doctorRun {
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
	at := now()

	cached, cacheErr := readUsageCache(path)
	if cacheErr != nil {
		note("doctor: ignoring usage cache: %v\n", cacheErr)
	}
	snaps := cached.Snapshots
	haveUsage := !cached.WrittenAt.IsZero()
	meta := "(no usage data; run agents usage)"
	if haveUsage {
		meta = "(usage from " + usg.HumanDuration(at.Sub(cached.WrittenAt)) + " ago; run agents usage to refresh)"
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
		Now:       at,
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
