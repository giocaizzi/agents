package apply

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestDiffRedactsOldSecrets pins that apply -n never prints the previous
// value of a secret the managed document carries.
func TestDiffRedactsOldSecrets(t *testing.T) {
	t.Parallel()
	t.Run("rotated secret in an array element", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "s.json")
		writeFileT(t, path, "{\n  \"s\": {\"args\": [\"--key\", \"old-token-1234\"]}\n}\n")
		e := engineIn(dir)
		e.DryRun = true
		res := runOne(t, e, Target{
			Path: path, Format: JSON,
			Managed:   map[string]any{"s": map[string]any{"args": []any{"--key", "new-token-5678"}}},
			Sensitive: []string{"new-token-5678"},
		})
		if res.Err != nil {
			t.Fatal(res.Err)
		}
		for _, tok := range []string{"old-token-1234", "new-token-5678"} {
			if strings.Contains(res.Diff, tok) {
				t.Errorf("diff leaks %s:\n%s", tok, res.Diff)
			}
		}
	})
	t.Run("retired managed key that held a secret", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "s.json")
		e := engineIn(dir)
		first := Target{
			Path: path, Format: JSON,
			Managed:   map[string]any{"h": map[string]any{"Authorization": "Bearer old-token-1234"}},
			Sensitive: []string{"old-token-1234"},
		}
		if res := runOne(t, e, first); res.Err != nil {
			t.Fatal(res.Err)
		}
		e.DryRun = true
		res := runOne(t, e, Target{Path: path, Format: JSON, Managed: map[string]any{"k": 1}})
		if res.Err != nil {
			t.Fatal(res.Err)
		}
		if strings.Contains(res.Diff, "old-token-1234") {
			t.Errorf("diff leaks the retired token:\n%s", res.Diff)
		}
	})
}
