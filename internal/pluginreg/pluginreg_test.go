package pluginreg_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/finding"
	"github.com/giocaizzi/agents/internal/pluginreg"
)

// checkout creates a registry checkout with the given plugin directories.
func checkout(t *testing.T, plugins ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, p := range plugins {
		if err := os.MkdirAll(filepath.Join(dir, "plugins", p), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestNames(t *testing.T) {
	t.Parallel()
	dir := checkout(t, "go", "api", ".hidden")
	if err := os.WriteFile(filepath.Join(dir, "plugins", "README.md"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := pluginreg.Names(dir), []string{"api", "go"}; !slices.Equal(got, want) {
		t.Errorf("Names = %v, want %v", got, want)
	}
	for _, d := range []string{"", filepath.Join(dir, "nope")} {
		if got := pluginreg.Names(d); got != nil {
			t.Errorf("Names(%q) = %v, want nil", d, got)
		}
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()
	harnesses := []string{"claude", "codex"}
	enabled := map[string][]string{"api": {"claude"}, "go": {"claude", "codex"}}
	tests := []struct {
		name string
		sel  func(t *testing.T) pluginreg.Selection
		want string // substring of the findings; "" means none
		warn bool   // the match must be a warning, not an error
	}{
		{
			name: "in step",
			sel: func(t *testing.T) pluginreg.Selection {
				return pluginreg.Selection{Checkout: checkout(t, "api", "go"), Enabled: enabled, Harnesses: harnesses}
			},
		},
		{
			name: "gone upstream",
			sel: func(t *testing.T) pluginreg.Selection {
				return pluginreg.Selection{Checkout: checkout(t, "api"), Enabled: enabled, Harnesses: harnesses}
			},
			want: "plugins.enabled.go: not a plugin of the registry checkout",
		},
		{
			name: "unknown harness",
			sel: func(t *testing.T) pluginreg.Selection {
				return pluginreg.Selection{Checkout: checkout(t, "api"), Enabled: map[string][]string{"api": {"emacs"}}, Harnesses: harnesses}
			},
			want: "unknown harness emacs",
		},
		{
			name: "disabled and enabled nowhere",
			sel: func(t *testing.T) pluginreg.Selection {
				return pluginreg.Selection{Checkout: checkout(t, "api"), Enabled: map[string][]string{"api": {"claude"}}, Disabled: []string{"hud"}, Harnesses: harnesses}
			},
			want: "hud is not enabled for any harness",
			warn: true,
		},
		{
			name: "no registry configured",
			sel: func(*testing.T) pluginreg.Selection {
				return pluginreg.Selection{Enabled: enabled, Harnesses: harnesses}
			},
			want: "registry checkout missing",
			warn: true,
		},
		{
			name: "checkout directory without plugins",
			sel: func(t *testing.T) pluginreg.Selection {
				return pluginreg.Selection{Checkout: t.TempDir(), Enabled: enabled, Harnesses: harnesses}
			},
			want: "registry checkout missing",
			warn: true,
		},
		{
			name: "nothing enabled needs no checkout",
			sel: func(*testing.T) pluginreg.Selection {
				return pluginreg.Selection{Harnesses: harnesses}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			findings := pluginreg.Check(tt.sel(t))
			lines := make([]string, len(findings))
			for i, f := range findings {
				lines[i] = f.String()
			}
			got := strings.Join(lines, "\n")
			if tt.want == "" {
				if got != "" {
					t.Errorf("Check = %q, want no findings", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("Check = %q, want it to contain %q", got, tt.want)
			}
			if gotErr := finding.HasErrors(findings); gotErr == tt.warn {
				t.Errorf("HasErrors = %v, want %v", gotErr, !tt.warn)
			}
		})
	}
}
