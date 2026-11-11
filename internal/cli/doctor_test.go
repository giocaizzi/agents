package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/doctor"
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
	return d, &out, &errb, filepath.Join(cacheHome, "agents", usageCacheFile)
}

// runDoctorFixed runs doctor with the fixed test clock and the default budget.
func runDoctorFixed(t *testing.T, d Deps, args ...string) int {
	t.Helper()
	return d.runDoctorBudget(args, fixedNow, doctorBudget)
}

func TestRunDoctorFlagError(t *testing.T) {
	t.Parallel()
	d, _, errb, _ := doctorEnv(t, "models-fresh")
	if code := runDoctorFixed(t, d, "-nope"); code != 2 {
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
	if code := runDoctorFixed(t, d); code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, errb)
	}
	for _, want := range []string{"agents doctor", "(no usage data; run agents usage)", "model catalog synced 309 days ago (2026-01-01)"} {
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
	if code := runDoctorFixed(t, d); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.HasPrefix(errb.String(), "doctor: ") || out.Len() != 0 {
		t.Errorf("stderr = %q, stdout = %q", errb, out)
	}
}

func TestRunDoctorQuiet(t *testing.T) {
	t.Parallel()
	d, out, errb, _ := doctorEnv(t, "models-fresh")
	if code := runDoctorFixed(t, d, "-q"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if out.Len() != 0 || errb.Len() != 0 {
		t.Errorf("-q printed stdout %q stderr %q, want nothing", out, errb)
	}
}

func TestRunDoctorQuietStillPrintsWarnings(t *testing.T) {
	t.Parallel()
	d, out, _, _ := doctorEnv(t, "models-stale")
	if code := runDoctorFixed(t, d, "-q"); code != 0 {
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
	// -json ignores -q.
	if code := runDoctorFixed(t, d, "-json", "-q"); code != 0 {
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

// Doctor reads the usage cache only: it states its age and never rewrites it.
func TestRunDoctorReadsCacheOnly(t *testing.T) {
	t.Parallel()
	d, out, errb, path := doctorEnv(t, "models-fresh")
	if err := writeUsageCache(path, sampleSnapshots(), usageNow.Add(-12*time.Minute)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if code := runDoctorFixed(t, d); code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, errb)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("cache changed (err %v)", err)
	}
	if !strings.Contains(out.String(), "(usage from 12m ago; run agents usage to refresh)") {
		t.Errorf("output = %q", out)
	}
}

func TestRunDoctorBudgetOverrun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{"default", nil, "doctor: timed out after 50ms\n"},
		{"quiet", []string{"-q"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, out, errb, _ := doctorEnv(t, "models-stale")
			release := make(chan struct{})
			// Let the abandoned worker go before the temp dirs do.
			t.Cleanup(func() { close(release) })
			blockedNow := func() time.Time { <-release; return usageNow }
			code := d.runDoctorBudget(tc.args, blockedNow, 50*time.Millisecond)
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
			if code := runDoctorFixed(t, d, args...); code != 0 {
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
