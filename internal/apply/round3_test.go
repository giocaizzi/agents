package apply

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestUnionKeepsEntriesTheUserHad(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	writeFileT(t, path, "{\n  \"u\": [\"x\"]\n}\n")
	e := engineIn(dir)
	union := [][]string{{"u"}}
	managed := map[string]any{"u": []any{"x", "y"}}
	// x is the user's; y is added by agents. The second run sees y already in
	// the file and must still know agents added it.
	for range 2 {
		if res := runOne(t, e, Target{Path: path, Format: JSON, Managed: managed, Union: union}); res.Err != nil {
			t.Fatal(res.Err)
		}
	}
	res := runOne(t, e, Target{Path: path, Format: JSON, Managed: map[string]any{"k": 1}, Union: union})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if got := readFileT(t, path); strings.Contains(got, `"y"`) || !strings.Contains(got, `"x"`) {
		t.Errorf("file = %q, want x kept and y gone", got)
	}
}

func TestUnionOldManifestListCountsAsAdded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	writeFileT(t, path, "{\n  \"u\": [\"a\"]\n}\n")
	e := engineIn(dir)
	state := fmt.Sprintf(`{"version":1,"files":{%s:{"owner":"t","format":"json","keys":[],"union":{%s:[%s]}}}}`,
		strconv.Quote(path), strconv.Quote(`["u"]`), strconv.Quote(`"a"`))
	writeFileT(t, e.StatePath, state)
	res := runOne(t, e, Target{Path: path, Format: JSON, Managed: map[string]any{"k": 1}, Union: [][]string{{"u"}}})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if got := readFileT(t, path); strings.Contains(got, `"a"`) {
		t.Errorf("file = %q, want the listed element retired", got)
	}
}
