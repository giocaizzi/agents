package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const claudeConfig = `schema_version = 1
harnesses = ["claude"]
`

func TestCheck_Ok(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, claudeConfig)
	if code := e.run("check", "-file", p); code != 0 {
		t.Fatalf("exit = %d, out = %s, err = %s", code, e.out, e.errOut)
	}
}

func TestCheck_ValidationError(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, claudeConfig+"[models]\ndefault = \"missing\"\n")
	if code := e.run("check", "-file", p); code != 1 {
		t.Fatalf("exit = %d, out = %s", code, e.out)
	}
	if !strings.Contains(e.out.String(), "models.default") {
		t.Errorf("out = %q", e.out)
	}

	if code := e.run("check", "-json", "-file", p); code != 1 {
		t.Fatalf("json exit = %d", code)
	}
	var got struct {
		Findings []map[string]string `json:"findings"`
	}
	if err := json.Unmarshal(e.out.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, e.out)
	}
	want := []map[string]string{{
		"where":    "models.default",
		"message":  `tier "missing" is not declared under models.tiers`,
		"severity": "error",
	}}
	if diff := cmp.Diff(want, got.Findings); diff != "" {
		t.Errorf("findings (-want +got):\n%s", diff)
	}
}

func TestCheck_JsonWithNoFindingsIsAnEmptyList(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, claudeConfig)
	if code := e.run("check", "-json", "-file", p); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if got := strings.Join(strings.Fields(e.out.String()), ""); got != `{"findings":[]}` {
		t.Errorf("out = %s", got)
	}
}

func TestCheck_Stdin(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, claudeConfig)
	if code := e.run("check", "-file", "-"); code != 0 {
		t.Fatalf("exit = %d, out = %s, err = %s", code, e.out, e.errOut)
	}
}

func TestCheck_LoadError(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, "bogus = 1\n")
	if code := e.run("check", "-file", p); code != 1 || e.errOut.Len() == 0 {
		t.Fatalf("exit = %d, stderr = %q", code, e.errOut)
	}
}

func TestCheck_BadFlag(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	if code := e.run("check", "-nope"); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestCheck_LiveDrift(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, claudeConfig)
	if code := e.run("check", "-live", "-file", p); code != 1 || !strings.Contains(e.out.String(), "differs from what apply would write (create)") {
		t.Fatalf("before apply: exit = %d, out = %s", code, e.out)
	}
	if code := e.run("apply", "-file", p); code != 0 {
		t.Fatalf("apply exit = %d, err = %s", code, e.errOut)
	}
	if code := e.run("check", "-live", "-file", p); code != 0 {
		t.Fatalf("after apply: exit = %d, out = %s", code, e.out)
	}
	if err := os.WriteFile(filepath.Join(e.home, ".claude", "settings.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := e.run("check", "-live", "-file", p); code != 1 || !strings.Contains(e.out.String(), "(update)") {
		t.Fatalf("after edit: exit = %d, out = %s", code, e.out)
	}
}
