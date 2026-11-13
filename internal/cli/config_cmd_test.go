package cli_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/giocaizzi/agents/internal/config"
)

const showConfig = `schema_version = 1
harnesses = ["claude", "codex"]
[models]
default = "main"
[models.tiers.main]
claude = "m1"
`

func TestConfigShow_Toml(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, showConfig)
	if code := e.run("config", "show", "-file", p); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	out := e.out.String()
	for _, want := range []string{"# files: " + p, "# harnesses (configured): claude, codex", "# profile: none"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	var cfg config.Config
	if err := toml.Unmarshal(e.out.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Models.Default != "main" || cfg.Models.Tiers["main"].Claude != "m1" {
		t.Errorf("round trip lost values: %+v", cfg.Models)
	}
}

func TestConfigShow_Json(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, showConfig)
	if code := e.run("config", "show", "-json", "-file", p); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var cfg config.Config
	if err := json.Unmarshal(e.out.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Models.Default != "main" {
		t.Errorf("models.default = %q", cfg.Models.Default)
	}
}

func TestConfigShow_Origin(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	p := e.writeConfig(t, showConfig)
	if code := e.run("config", "show", "-origin", "-profile", "", "-harnesses", "pi", "-file", p); code != 0 {
		t.Fatalf("exit = %d, err = %s", code, e.errOut)
	}
	lines := strings.Split(strings.TrimSpace(e.out.String()), "\n")
	for _, want := range []string{
		`models.default = "main"  # ` + p,
		`models.tiers.main.claude = "m1"  # ` + p,
		`schema_version = 1  # ` + p,
	} {
		if !slices.Contains(lines, want) {
			t.Errorf("missing line %q in:\n%s", want, e.out)
		}
	}
	if !sortedLines(lines) {
		t.Errorf("lines are not sorted:\n%s", e.out)
	}
}

func TestSchema(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	if code := e.run("schema"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	want, err := config.Schema()
	if err != nil {
		t.Fatal(err)
	}
	if e.out.String() != string(want) {
		t.Error("schema output differs from config.Schema()")
	}
}

func TestConfigShow_NeedsASubcommand(t *testing.T) {
	t.Parallel()
	e := newStdEnv(t, "")
	if code := e.run("config"); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func sortedLines(lines []string) bool {
	keys := make([]string, len(lines))
	for i, l := range lines {
		keys[i], _, _ = strings.Cut(l, " = ")
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			return false
		}
	}
	return true
}
