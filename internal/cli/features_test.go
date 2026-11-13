package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestFeatures_TextShowsTheMatrix(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	if code := e.run("features"); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	out := e.out.String()
	for _, want := range []string{"KEY", "claude", "antigravity", "permissions.allow_command_patterns", "harness.pi.default_model", "planned"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "schema_version") {
		t.Errorf("an internal key is listed:\n%s", out)
	}
}

func TestFeatures_HarnessesLimitTheColumns(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	if code := e.run("features", "-harnesses", "codex,claude"); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	header := strings.Fields(strings.SplitN(e.out.String(), "\n", 2)[0])
	if diff := cmp.Diff([]string{"KEY", "claude", "codex"}, header); diff != "" {
		t.Errorf("header (-want +got):\n%s", diff)
	}
	if strings.Contains(e.out.String(), "harness.pi.") || !strings.Contains(e.out.String(), "harness.codex.sandbox_mode") {
		t.Errorf("harness keys are not limited to the shown harnesses:\n%s", e.out)
	}

	if code := e.run("features", "-harnesses", "emacs"); code != 2 || !strings.Contains(e.errOut.String(), `unknown harness "emacs"`) {
		t.Errorf("exit = %d, err = %s", code, e.errOut)
	}
}

func TestFeatures_JSON(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	if code := e.run("features", "-json", "-harnesses", "codex"); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	var got struct {
		Harnesses []string `json:"harnesses"`
		Features  []struct {
			Key   string                       `json:"key"`
			Scope string                       `json:"scope"`
			Cells map[string]map[string]string `json:"cells"`
		} `json:"features"`
	}
	if err := json.Unmarshal(e.out.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, e.out)
	}
	if diff := cmp.Diff([]string{"codex"}, got.Harnesses); diff != "" {
		t.Errorf("harnesses (-want +got):\n%s", diff)
	}
	found := false
	for _, f := range got.Features {
		for h := range f.Cells {
			if h != "codex" {
				t.Errorf("%s lists a cell for %s", f.Key, h)
			}
		}
		if f.Key == "permissions.reasons" {
			found = true
			if c := f.Cells["codex"]; c["status"] != "planned" || c["evidence"] == "" {
				t.Errorf("permissions.reasons codex cell = %v", c)
			}
		}
	}
	if !found {
		t.Error("permissions.reasons is missing")
	}
}

func TestCheck_ReportsKeysAHarnessIgnores(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, `schema_version = 1
harnesses = ["claude", "pi", "codex"]

[context]
max_tokens = 4096
auto_compact_window = 500000
`)
	if code := e.run("check", "-json", "-file", p); code != 0 {
		t.Fatalf("exit = %d, out = %s", code, e.out)
	}
	var got struct {
		Findings []map[string]string `json:"findings"`
	}
	if err := json.Unmarshal(e.out.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, e.out)
	}
	bySeverity := map[string]map[string]string{}
	for _, f := range got.Findings {
		bySeverity[f["where"]+"/"+f["severity"]] = f
	}
	if len(got.Findings) != 4 {
		t.Fatalf("findings = %v, want one warning and one info for each of the two keys", got.Findings)
	}
	warn := bySeverity["context.max_tokens/warning"]["message"]
	if !strings.HasPrefix(warn, "no effect on claude (") || !strings.Contains(warn, "CLAUDE_CODE_MAX_OUTPUT_TOKENS") {
		t.Errorf("planned message = %q", warn)
	}
	info := bySeverity["context.max_tokens/info"]["message"]
	if !strings.HasPrefix(info, "no effect on codex (") || !strings.Contains(info, "; pi (") {
		t.Errorf("unsupported message = %q: want every harness once", info)
	}
	if m := bySeverity["context.auto_compact_window/warning"]["message"]; !strings.HasPrefix(m, "no effect on pi (") || strings.Contains(m, "claude") {
		t.Errorf("auto_compact_window warning = %q, want pi only", m)
	}
	if m := bySeverity["context.auto_compact_window/info"]["message"]; !strings.HasPrefix(m, "no effect on codex (") || strings.Contains(m, "claude") {
		t.Errorf("auto_compact_window info = %q, want codex only", m)
	}
}

func TestCheck_IgnoresHarnessesThatAreNotSelected(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, `schema_version = 1
harnesses = ["claude"]

[context]
auto_compact_window = 500000

[harness.pi]
default_model = "model"
`)
	if code := e.run("check", "-file", p); code != 0 || !strings.Contains(e.out.String(), "config ok") {
		t.Fatalf("exit = %d, out = %s", code, e.out)
	}
}

func TestApply_TelemetryHostIsTheShortName(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	e.deps.Hostname = func() (string, error) { return "box.local", nil }
	p := e.writeConfig(t, `schema_version = 1
harnesses = ["claude"]

[telemetry]
enabled = true
endpoint = "https://otel.example.com"
`)
	if code := e.run("apply", "-file", p); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	raw, err := os.ReadFile(filepath.Join(e.home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	attrs := settings.Env["OTEL_RESOURCE_ATTRIBUTES"]
	if !strings.Contains(attrs, "host.name=box,") || strings.Contains(attrs, "local") {
		t.Errorf("OTEL_RESOURCE_ATTRIBUTES = %q, want host.name=box", attrs)
	}
}
