package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateHelp = flag.Bool("update-help", false, "update CLI help goldens")

func helpDeps(out, err *bytes.Buffer) Deps {
	return Deps{Stdout: out, Stderr: err, Getenv: func(key string) string {
		switch key {
		case "HARNESS_DIR", "NO_COLOR", "TERM":
			return ""
		default:
			panic("help accessed execution environment: " + key)
		}
	}}
}

func TestHelpNodes(t *testing.T) {
	t.Parallel()
	paths := []string{"", "init", "check", "config", "config show", "schema", "render", "apply", "prompt", "prompt edit", "prompt show", "prompt matrix", "prompt check", "mcp", "mcp sync", "mcp add", "models", "models tiers", "models sync", "plugins", "plugins list", "plugins update", "plugins prune", "usage", "doctor", "version", "help"}
	for _, path := range paths {
		for _, spelling := range []string{"-h", "--help", "explicit"} {
			t.Run(path+"/"+spelling, func(t *testing.T) {
				t.Parallel()
				args := strings.Fields(path)
				if spelling == "explicit" {
					args = append([]string{"help"}, args...)
				} else {
					args = append(args, spelling)
				}
				assertLocalHelp(t, path, args)
			})
		}
	}
}

func assertLocalHelp(t *testing.T, path string, args []string) {
	t.Helper()
	var out, err bytes.Buffer
	d := helpDeps(&out, &err)
	code := d.Run(t.Context(), args)
	if code != 0 || err.Len() != 0 {
		t.Fatalf("code=%d stderr=%q stdout=%q", code, &err, &out)
	}
	want := strings.TrimSpace("agents " + path)
	if !strings.Contains(out.String(), want) || !strings.Contains(out.String(), "USAGE") {
		t.Errorf("missing local usage %q: %s", want, &out)
	}
	if path != "" && path != "help" && strings.Contains(out.String(), "agents doctor") && path != "doctor" {
		t.Errorf("unrelated doctor help: %s", &out)
	}
	// Every spelling must render the same parser-derived local help.
	var explicit, explicitErr bytes.Buffer
	other := helpDeps(&explicit, &explicitErr)
	if code := other.Run(t.Context(), append([]string{"help"}, strings.Fields(path)...)); code != 0 || explicitErr.Len() != 0 {
		t.Fatalf("explicit help code=%d stderr=%q", code, &explicitErr)
	}
	if out.String() != explicit.String() {
		t.Errorf("help forms differ:\nexplicit: %s\nactual: %s", &explicit, &out)
	}
}

func TestHelpGoldens(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"", "mcp", "models", "plugins", "render", "mcp add", "plugins update", "prompt"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			var out, err bytes.Buffer
			d := helpDeps(&out, &err)
			if code := d.Run(t.Context(), append([]string{"help"}, strings.Fields(path)...)); code != 0 {
				t.Fatalf("code=%d: %s", code, &err)
			}
			name := strings.ReplaceAll(path, " ", "_")
			if name == "" {
				name = "root"
			}
			file := filepath.Join("testdata", "help_"+name+".golden")
			if *updateHelp {
				if e := os.WriteFile(file, out.Bytes(), 0o600); e != nil {
					t.Fatal(e)
				}
			}
			want, e := os.ReadFile(file)
			if e != nil {
				t.Fatal(e)
			}
			if out.String() != string(want) {
				t.Errorf("help differs from %s (-want +got):\n-%s\n+%s", file, want, &out)
			}
		})
	}
}

func TestHelpLocalErrors(t *testing.T) {
	t.Parallel()
	tests := []struct{ args, local, absent string }{
		{"bogus", "agents", "EXAMPLES"},
		{"help mcp bogus", "agents mcp", "agents doctor"},
		{"mcp", "agents mcp", "OPTIONS"},
		{"mcp bogus", "agents mcp", "agents doctor"},
		{"models bogus", "agents models", "agents doctor"},
		{"plugins bogus", "agents plugins", "agents doctor"},
		{"render -target output", "agents render", "agents doctor"},
		{"mcp add -bogus", "agents mcp add", "agents doctor"},
		{"plugins update -bogus", "agents plugins update", "agents doctor"},
		{"init -bogus", "agents init", "agents doctor"},
		{"check -file", "agents check", "agents doctor"},
		{"version -bogus", "agents version", "agents doctor"},
	}
	for _, tt := range tests {
		t.Run(tt.args, func(t *testing.T) {
			t.Parallel()
			var out, err bytes.Buffer
			d := helpDeps(&out, &err)
			if code := d.Run(t.Context(), strings.Fields(tt.args)); code != 2 || out.Len() != 0 {
				t.Fatalf("code=%d out=%q err=%q", code, &out, &err)
			}
			if !strings.Contains(err.String(), tt.local) || strings.Contains(err.String(), tt.absent) {
				t.Errorf("non-local error: %s", &err)
			}
		})
	}
}

func TestHelpGroupAliases(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"mcp", "models", "plugins"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			assertLocalHelp(t, path, []string{path, "help"})
		})
	}
}

func TestHelpParserBoundaries(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"render", "-file", "--help"},
		{"render", "-file=--help"},
		{"render", "--", "--help"},
		{"render", "positional", "--help"},
		{"models", "-file", "--help"},
		{"plugins", "-file", "--help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			var out, err bytes.Buffer
			d := Deps{Stdout: &out, Stderr: &err, Getenv: func(string) string { return "" }}
			if code := d.Run(t.Context(), args); code != 1 {
				t.Errorf("code=%d, want execution error 1; out=%s err=%s", code, &out, &err)
			}
			if strings.Contains(out.String()+err.String(), "USAGE") {
				t.Errorf("argument or flag value interpreted as help: %s %s", &out, &err)
			}
		})
	}
}

func TestHelpMCPForwardedArguments(t *testing.T) {
	t.Parallel()
	for _, forwarded := range [][]string{{"--help"}, {"-h"}, {"--", "--help"}} {
		t.Run(strings.Join(forwarded, " "), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			var out, err bytes.Buffer
			d := Deps{Stdout: &out, Stderr: &err, Getenv: func(string) string { return "" }}
			args := append([]string{"mcp", "add", "-dir", dir, "-no-sync", "example", "example-server"}, forwarded...)
			if code := d.Run(t.Context(), args); code != 0 {
				t.Fatalf("code=%d: %s", code, &err)
			}
			data, e := os.ReadFile(filepath.Join(dir, ".mcp.json"))
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Contains(data, []byte("--help")) && !bytes.Contains(data, []byte("-h")) {
				t.Errorf("lost forwarded arguments: %s", data)
			}
			if strings.Contains(out.String(), "USAGE") {
				t.Errorf("forwarded argument interpreted as help: %s", &out)
			}
		})
	}
}

func TestHelpRootScope(t *testing.T) {
	t.Parallel()
	var out, err bytes.Buffer
	d := helpDeps(&out, &err)
	if code := d.Run(t.Context(), []string{"--help"}); code != 0 || err.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, &err)
	}
	text := out.String()
	for _, want := range []string{"-h, --help", "-v, --version"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing root flag %q: %s", want, text)
		}
	}
	for _, absent := range []string{"EXAMPLES", "ENVIRONMENT", "-file", "-harness", "-provider"} {
		if strings.Contains(text, absent) {
			t.Errorf("unrelated root content %q: %s", absent, text)
		}
	}
	_, commands, ok := strings.Cut(text, "COMMANDS\n")
	if !ok {
		t.Fatal("missing command list")
	}
	commands, _, _ = strings.Cut(commands, "\n\n")
	var names []string
	for line := range strings.SplitSeq(commands, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			names = append(names, fields[0])
		}
	}
	want := "init check config schema render apply prompt mcp models plugins usage doctor version help"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("command list=%q, want %q", got, want)
	}
	if strings.Count(text, "COMMANDS\n") != 1 {
		t.Error("command list repeated")
	}
}

func TestHelpLeafErrors(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"init", "check", "render", "prompt", "mcp sync", "mcp add", "models sync", "plugins list", "plugins update", "plugins prune", "usage", "doctor", "version", "help"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			var out, err bytes.Buffer
			d := helpDeps(&out, &err)
			if code := d.Run(t.Context(), append(strings.Fields(path), "-bogus")); code != 2 || out.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, &out, &err)
			}
			if !strings.Contains(err.String(), "agents "+path) || !strings.Contains(err.String(), "USAGE") {
				t.Errorf("missing local error help: %s", &err)
			}
		})
	}
}

func TestHelpPluginAliases(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"-n", "-f"}, {"-dry-run", "-force"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			var out, err bytes.Buffer
			d := helpDeps(&out, &err)
			flags, e := d.parsePluginsFlags("plugins list", args)
			if e != nil || !flags.dryRun || !flags.force || flags.file != "" || flags.jsonOutput {
				t.Errorf("flags=%+v err=%v", flags, e)
			}
		})
	}
}

func TestHelpBareGroupsStillExecute(t *testing.T) {
	// Discovery falls back to the real home and relative paths: isolate both.
	dir := t.TempDir()
	t.Chdir(dir)
	for _, command := range []string{"models", "plugins"} {
		t.Run(command, func(t *testing.T) {
			var out, err bytes.Buffer
			d := Deps{Stdout: &out, Stderr: &err, Getenv: func(key string) string {
				if key == "HOME" || key == "CHEZMOI_SOURCE_DIR" || key == "XDG_CACHE_HOME" {
					return dir
				}
				return ""
			}}
			if code := d.Run(t.Context(), []string{command}); code != 1 {
				t.Errorf("code=%d, want configuration discovery error 1: %s %s", code, &out, &err)
			}
			if err.Len() == 0 || strings.Contains(out.String()+err.String(), "USAGE") {
				t.Errorf("bare group did not execute default action: %s %s", &out, &err)
			}
		})
	}
}

func TestHelpVersionAliases(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"version", "-v", "--version"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			var out, err bytes.Buffer
			d := helpDeps(&out, &err)
			d.Version = "1.2.3"
			if code := d.Run(t.Context(), []string{command}); code != 0 || out.String() != "agents 1.2.3\n" || err.Len() != 0 {
				t.Errorf("code=%d stdout=%q stderr=%q", code, &out, &err)
			}
		})
	}
}

func TestHelpBeforeWrites(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"init", "-dir"}, {"mcp", "add", "-dir"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			var out, err bytes.Buffer
			d := helpDeps(&out, &err)
			invocation := append(append([]string{}, args...), dir, "--help")
			if code := d.Run(t.Context(), invocation); code != 0 {
				t.Fatalf("code=%d: %s", code, &err)
			}
			entries, e := os.ReadDir(dir)
			if e != nil {
				t.Fatal(e)
			}
			if len(entries) != 0 {
				t.Errorf("help wrote files: %v", entries)
			}
		})
	}
}
