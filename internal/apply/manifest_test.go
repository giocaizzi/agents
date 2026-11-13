package apply

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestManifestEmptyContainerLeafIsIgnored(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	e := engineIn(dir)
	writeFileT(t, path, `{"mcpServers": {"mine": {"command": "x"}}, "tags": []}`)
	// A manifest from before empty containers stopped being recorded.
	writeFileT(t, e.StatePath, `{"version":1,"files":{`+strconv.Quote(path)+`:{"keys":[["mcpServers"],["tags"]]}}}`)
	managed := map[string]any{"mcpServers": map[string]any{"docs": map[string]any{"url": "u"}}}
	res := runOne(t, e, Target{Path: path, Format: JSON, Managed: managed})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	got := readFileT(t, path)
	if !strings.Contains(got, `"mine"`) || !strings.Contains(got, `"tags"`) || !strings.Contains(got, `"docs"`) {
		t.Errorf("user entries lost or managed key missing:\n%s", got)
	}
	// And the manifest no longer lists the container.
	if m := readFileT(t, e.StatePath); strings.Contains(m, `"mcpServers"
`) || strings.Contains(m, `"tags"`) {
		t.Errorf("manifest still records a container:\n%s", m)
	}
}

func TestLegacyOnlyOnFirstApply(t *testing.T) {
	t.Parallel()
	legacy := [][]string{{"old", "key"}}
	managed := map[string]any{"new": 1}

	t.Run("first apply deletes", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "s.json")
		writeFileT(t, path, `{"old": {"key": 1, "other": 2}}`)
		if res := runOne(t, engineIn(dir), Target{Path: path, Format: JSON, Managed: managed, Legacy: legacy}); res.Err != nil {
			t.Fatal(res.Err)
		}
		got := readFileT(t, path)
		if strings.Contains(got, `"key"`) || !strings.Contains(got, `"other"`) {
			t.Errorf("legacy key not removed alone:\n%s", got)
		}
	})

	t.Run("later apply keeps a user-set key", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "s.json")
		writeFileT(t, path, `{}`)
		e := engineIn(dir)
		tg := Target{Path: path, Format: JSON, Managed: managed, Legacy: legacy}
		if res := runOne(t, e, tg); res.Err != nil {
			t.Fatal(res.Err)
		}
		// The user sets the same path afterwards.
		writeFileT(t, path, `{"old": {"key": 7}, "new": 1}`)
		if res := runOne(t, e, tg); res.Err != nil || res.Status != StatusUnchanged {
			t.Fatalf("result = %+v", res)
		}
		if got := readFileT(t, path); !strings.Contains(got, `"key": 7`) {
			t.Errorf("user key deleted on a later run:\n%s", got)
		}
	})
}
