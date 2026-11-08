package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/doctor"
	"github.com/giocaizzi/agents/internal/provider"
)

// doctorEnv is a Deps whose source dir holds the doctor testdata catalog
// (testdata/doctor/<catalog>.json) and policy, and whose cache dir is empty.
func doctorEnv(t *testing.T, catalog string) (Deps, *bytes.Buffer, *bytes.Buffer, string) {
	t.Helper()
	src, cacheHome := t.TempDir(), t.TempDir()
	data := filepath.Join(src, ".chezmoidata")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	for from, to := range map[string]string{catalog + ".json": "models.json", "harness-policy.toml": "harness-policy.toml"} {
		b, err := os.ReadFile(filepath.Join("testdata", "doctor", from))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(data, to), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out, errb bytes.Buffer
	d := Deps{
		Stdout: &out, Stderr: &errb, GOOS: runtime.GOOS,
		Getenv: func(k string) string {
			switch k {
			case "CHEZMOI_SOURCE_DIR":
				return src
			case "XDG_CACHE_HOME":
				return cacheHome
			}
			return ""
		},
	}
	return d, &out, &errb, filepath.Join(cacheHome, "chezmoi-agents", usageCacheFile)
}

// countingStore counts every access to the credential store.
type countingStore struct {
	provider.Store

	calls atomic.Int64
}

func (s *countingStore) ReadFile(p string) ([]byte, error) {
	s.calls.Add(1)
	return s.Store.ReadFile(p)
}

func (s *countingStore) Secret(a, b string) (string, error) {
	s.calls.Add(1)
	return s.Store.Secret(a, b)
}
func (s *countingStore) Exists(p string) bool      { s.calls.Add(1); return s.Store.Exists(p) }
func (s *countingStore) ListDir(p string) []string { s.calls.Add(1); return s.Store.ListDir(p) }
func (s *countingStore) Getenv(k string) string    { s.calls.Add(1); return s.Store.Getenv(k) }
func (s *countingStore) Canonical(p string) string { s.calls.Add(1); return s.Store.Canonical(p) }
func (s *countingStore) SameFile(a, b string) bool { s.calls.Add(1); return s.Store.SameFile(a, b) }
func (s *countingStore) OpencodeCredentials(p string) ([]provider.OpencodeRow, error) {
	s.calls.Add(1)
	return s.Store.OpencodeCredentials(p)
}

func TestRunDoctorFlagError(t *testing.T) {
	t.Parallel()
	d, _, errb, _ := doctorEnv(t, "models-fresh")
	if code := d.runDoctorWith(t.Context(), []string{"-nope"}, fakeStore{}, testFetcher(t, nil)); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "flag parse error") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestRunDoctorDispatch(t *testing.T) {
	t.Parallel()
	d, out, _, _ := doctorEnv(t, "models-fresh")
	_ = out
	if code := d.Run(t.Context(), []string{"doctor", "-bogus"}); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestRunDoctorStaleCatalog(t *testing.T) {
	t.Parallel()
	d, out, errb, _ := doctorEnv(t, "models-stale")
	f := testFetcher(t, nil)
	if code := d.runDoctorWith(t.Context(), nil, fakeStore{home: f.home}, f); code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, errb)
	}
	for _, want := range []string{"chezmoi agents doctor", "(no usage cache; -live to refresh)", "model catalog synced 309 days ago (2026-01-01)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestRunDoctorLoadError(t *testing.T) {
	t.Parallel()
	d, out, errb, _ := doctorEnv(t, "models-fresh")
	empty := t.TempDir()
	base := d.Getenv
	d.Getenv = func(k string) string {
		if k == "CHEZMOI_SOURCE_DIR" {
			return empty
		}
		return base(k)
	}
	f := testFetcher(t, nil)
	if code := d.runDoctorWith(t.Context(), nil, fakeStore{home: f.home}, f); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.HasPrefix(errb.String(), "doctor: ") || out.Len() != 0 {
		t.Errorf("stderr = %q, stdout = %q", errb, out)
	}
}

func TestRunDoctorQuiet(t *testing.T) {
	t.Parallel()
	d, out, errb, _ := doctorEnv(t, "models-fresh")
	f := testFetcher(t, nil)
	if code := d.runDoctorWith(t.Context(), []string{"-q"}, fakeStore{home: f.home}, f); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if out.Len() != 0 || errb.Len() != 0 {
		t.Errorf("-q printed stdout %q stderr %q, want nothing", out, errb)
	}
}

func TestRunDoctorQuietStillPrintsWarnings(t *testing.T) {
	t.Parallel()
	d, out, _, _ := doctorEnv(t, "models-stale")
	f := testFetcher(t, nil)
	if code := d.runDoctorWith(t.Context(), []string{"-q"}, fakeStore{home: f.home}, f); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "model catalog synced") {
		t.Errorf("-q hid a warning:\n%s", out)
	}
}

type jsonOut struct {
	Findings []doctor.Finding `json:"findings"`
}

func TestRunDoctorJSON(t *testing.T) {
	t.Parallel()
	d, out, _, _ := doctorEnv(t, "models-stale")
	f := testFetcher(t, nil)
	// -json ignores -q.
	if code := d.runDoctorWith(t.Context(), []string{"-json", "-q"}, fakeStore{home: f.home}, f); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var got jsonOut
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got.Findings) == 0 || got.Findings[0].Kind != doctor.KindStaleCatalog || got.Findings[0].Level != doctor.LevelWarn {
		t.Errorf("findings = %+v, want the stale-catalog warning first", got.Findings)
	}
	if !strings.HasPrefix(out.String(), "{\n  \"findings\": [\n    {\n") || !strings.HasSuffix(out.String(), "\n}\n") {
		t.Errorf("not 2-space indented with a trailing newline:\n%s", out)
	}
}

func TestWriteDoctorJSONEmptyList(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := writeDoctorJSON(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff("{\n  \"findings\": []\n}\n", buf.String()); diff != "" {
		t.Errorf("empty JSON (-want +got):\n%s", diff)
	}
}

// The default view reads the cache only: no store access, no request.
func TestRunDoctorOfflineTouchesNothing(t *testing.T) {
	t.Parallel()
	d, out, errb, path := doctorEnv(t, "models-fresh")
	var reqs atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reqs.Add(1) }))
	t.Cleanup(srv.Close)
	f := testFetcher(t, srv)
	f.agyPath = func() (string, error) { reqs.Add(1); return "", os.ErrNotExist }
	if err := writeUsageCache(path, sampleSnapshots(), usageNow.Add(-12*time.Minute)); err != nil {
		t.Fatal(err)
	}
	store := &countingStore{Store: fakeStore{home: f.home}}

	if code := d.runDoctorWith(t.Context(), nil, store, f); code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, errb)
	}
	if n := store.calls.Load(); n != 0 {
		t.Errorf("store accessed %d times, want 0", n)
	}
	if n := reqs.Load(); n != 0 {
		t.Errorf("fetcher used %d times, want 0", n)
	}
	if !strings.Contains(out.String(), "(cached 12m ago; -live to refresh)") {
		t.Errorf("output = %q", out)
	}
}

func TestRunDoctorLiveRewritesCache(t *testing.T) {
	t.Parallel()
	d, out, errb, path := doctorEnv(t, "models-fresh")
	f := testFetcher(t, nil)
	if code := d.runDoctorWith(t.Context(), []string{"-live"}, fakeStore{home: f.home}, f); code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, errb)
	}
	if !strings.Contains(out.String(), "(live)") {
		t.Errorf("output = %q", out)
	}
	c, err := readUsageCache(path)
	if err != nil || !c.WrittenAt.Equal(usageNow) {
		t.Errorf("cache = %+v, err %v; want written at %v", c, err, usageNow)
	}
}

// blockingStore blocks every access until released: a provider lookup that hangs.
type blockingStore struct {
	fakeStore

	release chan struct{}
}

func (s blockingStore) ReadFile(string) ([]byte, error) { <-s.release; return nil, os.ErrNotExist }
func (s blockingStore) Exists(string) bool              { <-s.release; return false }
func (s blockingStore) ListDir(string) []string         { <-s.release; return nil }
func (s blockingStore) Getenv(string) string            { <-s.release; return "" }
func (s blockingStore) Secret(string, string) (string, error) {
	<-s.release
	return "", os.ErrNotExist
}

func TestRunDoctorBudgetOverrun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{"default", []string{"-live"}, "doctor: timed out after 50ms\n"},
		{"quiet", []string{"-live", "-q"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, out, errb, _ := doctorEnv(t, "models-stale")
			f := testFetcher(t, nil)
			release := make(chan struct{})
			t.Cleanup(func() {
				// Let the abandoned worker go before the temp dirs do.
				close(release)
			})
			b := doctorBudget{offline: time.Hour, live: 50 * time.Millisecond, perProvider: time.Second, identity: time.Second}
			code := d.runDoctorBudget(t.Context(), tc.args, blockingStore{fakeStore{home: f.home}, release}, f, b)
			if code != 0 {
				t.Errorf("exit = %d, want 0", code)
			}
			if out.Len() != 0 {
				t.Errorf("stdout = %q, want empty", out)
			}
			if errb.String() != tc.wantStderr {
				t.Errorf("stderr = %q, want %q", errb, tc.wantStderr)
			}
		})
	}
}

// The account email of the snapshots never reaches the text, -q or JSON output,
// though account-derived findings (capacity, accounts, headroom) exist.
func TestRunDoctorNeverPrintsAccountEmail(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"-q"}, {"-json"}} {
		t.Run(strings.Join(args, ""), func(t *testing.T) {
			t.Parallel()
			d, out, errb, path := doctorEnv(t, "models-fresh")
			snaps := sampleSnapshots()
			for i := range snaps {
				if snaps[i].Provider == "openai" {
					snaps[i].Limits[0].UsedPct = new(95.0)
				}
			}
			if err := writeUsageCache(path, snaps, usageNow.Add(-time.Minute)); err != nil {
				t.Fatal(err)
			}
			f := testFetcher(t, nil)
			if code := d.runDoctorWith(t.Context(), args, fakeStore{home: f.home}, f); code != 0 {
				t.Fatalf("exit = %d, stderr %q", code, errb)
			}
			checkNoEmail(t, out.String()+errb.String())
			if !strings.Contains(out.String(), "95%") {
				t.Errorf("no capacity finding to check against:\n%s", out)
			}
		})
	}
}

func checkNoEmail(t *testing.T, got string) {
	t.Helper()
	if got == "" || strings.Contains(got, "me@example.com") {
		t.Errorf("output empty or holding the account email:\n%s", got)
	}
}

// A live run that overruns its budget leaves neither usage-*.tmp nor a cache
// behind, even when its worker resumes afterwards.
func TestRunDoctorLiveOverrunLeavesNoTemp(t *testing.T) {
	t.Parallel()
	d, _, _, path := doctorEnv(t, "models-stale")
	f := testFetcher(t, nil)
	release := make(chan struct{})
	b := doctorBudget{offline: time.Hour, live: 50 * time.Millisecond, perProvider: time.Second, identity: time.Second}
	if code := d.runDoctorBudget(t.Context(), []string{"-live"}, blockingStore{fakeStore{home: f.home}, release}, f, b); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	close(release)
	// Give the released worker time to run to its end.
	time.Sleep(300 * time.Millisecond)
	if m, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "usage-*.tmp")); len(m) != 0 {
		t.Errorf("leftover temp files: %v", m)
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("cache written after the budget overran")
	}
}

func TestSplitHarnesses(t *testing.T) {
	t.Parallel()
	got := splitHarnesses(" pi,claude ,, codex\tpi\n")
	if diff := cmp.Diff([]string{"pi", "claude", "codex"}, got); diff != "" {
		t.Errorf("splitHarnesses (-want +got):\n%s", diff)
	}
	if got := splitHarnesses(""); len(got) != 0 {
		t.Errorf("splitHarnesses(\"\") = %v, want none", got)
	}
}

func TestOrderHarnesses(t *testing.T) {
	t.Parallel()
	got := orderHarnesses([]string{"zed", "pi", "opencode", "acme", "antigravity", "codex", "copilot", "claude"})
	want := []string{"claude", "copilot", "codex", "antigravity", "opencode", "pi", "acme", "zed"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("orderHarnesses (-want +got):\n%s", diff)
	}
}

func TestSnapshotHarnesses(t *testing.T) {
	t.Parallel()
	got := orderHarnesses(snapshotHarnesses(sampleSnapshots()))
	if len(got) == 0 || got[0] != "claude" && got[0] != "codex" {
		t.Errorf("harnesses = %v, want the union of the snapshots'", got)
	}
}
