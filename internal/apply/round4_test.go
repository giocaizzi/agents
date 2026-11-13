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

// TestDiffMasksRetiredURLSecret pins that a retired managed value never
// prints, whatever its path, here a URL whose query holds a secret.
func TestDiffMasksRetiredURLSecret(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	e := engineIn(dir)
	first := Target{
		Path: path, Format: JSON,
		Managed: map[string]any{"mcpServers": map[string]any{"s": map[string]any{
			"url":  "https://mcp.example.com/mcp?api_key=old-token-1234",
			"args": []any{"https://mcp.example.com/mcp?api_key=old-token-1234&x=<y>"},
		}}},
		Sensitive: []string{"old-token-1234"},
	}
	if res := runOne(t, e, first); res.Err != nil {
		t.Fatal(res.Err)
	}
	e.DryRun = true
	res := runOne(t, e, Target{Path: path, Format: JSON, Managed: map[string]any{"mcpServers": map[string]any{"t": map[string]any{"command": "x"}}}})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if strings.Contains(res.Diff, "old-token-1234") {
		t.Errorf("diff leaks the retired URL secret:\n%s", res.Diff)
	}
}

// TestDiffMaskingIsStructural pins that a short retired value masks only the
// removed line that held it, not context lines, + lines or other words.
func TestDiffMaskingIsStructural(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	e := engineIn(dir)
	first := Target{
		Path: path, Format: JSON,
		Managed: map[string]any{"mcpServers": map[string]any{
			"a": map[string]any{"command": "npx", "args": []any{"-y", "pkg-a"}},
			"b": map[string]any{"command": "npx", "args": []any{"-y", "pkg-b"}},
		}},
	}
	if res := runOne(t, e, first); res.Err != nil {
		t.Fatal(res.Err)
	}
	e.DryRun = true
	res := runOne(t, e, Target{
		Path: path, Format: JSON,
		Managed: map[string]any{"mcpServers": map[string]any{
			"b": map[string]any{"command": "npx", "args": []any{"-y", "pkg-c"}},
		}},
	})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	for line := range strings.SplitSeq(res.Diff, "\n") {
		if strings.HasPrefix(line, "+") && strings.Contains(line, removedMark) {
			t.Errorf("+ line masked: %q", line)
		}
		if strings.HasPrefix(line, " ") && strings.Contains(line, removedMark) {
			t.Errorf("context line masked: %q", line)
		}
	}
	if !strings.Contains(res.Diff, "\"npx\"") || !strings.Contains(res.Diff, "pkg-c") {
		t.Errorf("unrelated lines altered:\n%s", res.Diff)
	}
	if strings.Contains(res.Diff, "pkg-a") {
		t.Errorf("retired value printed:\n%s", res.Diff)
	}
}

// TestDiffMasksRetiredSecretKeptUnmanaged: a retired managed secret that
// still sits at an unmanaged path of the new file (shown there as
// «unmanaged») must not print raw on the - line that held it.
func TestDiffMasksRetiredSecretKeptUnmanaged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	writeFileT(t, path, "{\n  \"mine\": {\"Authorization\": \"Bearer old-token-1234\"}\n}\n")
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
}
