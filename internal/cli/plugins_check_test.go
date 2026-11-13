package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/finding"
)

func TestPluginFindings(t *testing.T) {
	t.Parallel()
	const missing = "registry checkout missing; run agents plugins update"
	tests := []struct {
		name     string
		plugins  config.Plugins
		checkout []string // plugin directories to create
		noTree   bool     // leave the checkout missing
		want     []finding.Finding
	}{
		{
			name:     "clean",
			plugins:  config.Plugins{Registry: "acme/harness", Enabled: map[string][]config.Harness{"dev-tools": {config.Claude}}},
			checkout: []string{"dev-tools"},
		},
		{
			name:    "missing checkout warns",
			plugins: config.Plugins{Registry: "acme/harness", Enabled: map[string][]config.Harness{"dev-tools": {config.Claude}}},
			noTree:  true,
			want:    []finding.Finding{finding.Warn("plugins", "%s", missing)},
		},
		{
			name:     "plugin absent from the checkout",
			plugins:  config.Plugins{Registry: "acme/harness", Enabled: map[string][]config.Harness{"ghost": {config.Claude}}},
			checkout: []string{"dev-tools"},
			want:     []finding.Finding{finding.New("plugins.enabled.ghost", "not a plugin of the registry checkout")},
		},
		{
			name:     "unknown harness",
			plugins:  config.Plugins{Registry: "acme/harness", Enabled: map[string][]config.Harness{"dev-tools": {"emacs"}}},
			checkout: []string{"dev-tools"},
			want:     []finding.Finding{finding.New("plugins.enabled.dev-tools", "unknown harness emacs")},
		},
		{
			name: "disabled plugin enabled nowhere",
			plugins: config.Plugins{
				Registry: "acme/harness", Disabled: []string{"guard"},
				Enabled: map[string][]config.Harness{"dev-tools": {config.Claude}},
			},
			checkout: []string{"dev-tools"},
			want:     []finding.Finding{finding.Warn("plugins.disabled", "guard is not enabled for any harness")},
		},
		{
			name: "disabled plugin that is enabled is fine",
			plugins: config.Plugins{
				Registry: "acme/harness", Disabled: []string{"dev-tools"},
				Enabled: map[string][]config.Harness{"dev-tools": {config.Claude}},
			},
			checkout: []string{"dev-tools"},
		},
		{name: "nothing configured", plugins: config.Plugins{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dirs := config.Dirs{Data: t.TempDir()}
			r := &config.Resolved{Config: config.Config{Plugins: tt.plugins}, Dirs: dirs}
			if !tt.noTree {
				for _, name := range tt.checkout {
					if err := os.MkdirAll(filepath.Join(dirs.Data, "registries", "harness", "plugins", name), 0o700); err != nil {
						t.Fatal(err)
					}
				}
			}
			got := Deps{}.pluginFindings(t.Context(), r)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("findings (-want +got):\n%s", diff)
			}
		})
	}
}
