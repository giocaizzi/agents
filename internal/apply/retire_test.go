package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func retireManaged() map[string]any {
	return map[string]any{"mcpServers": map[string]any{"docs": map[string]any{"url": "u"}}, "theme": "x"}
}

func TestRetireKeepsUserKeysAndFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	writeFileT(t, path, "{\n  \"mine\": 1\n}\n")
	e := engineIn(dir)
	if res := runOne(t, e, Target{Path: path, Format: JSON, Managed: retireManaged()}); res.Err != nil {
		t.Fatal(res.Err)
	}
	// The target is no longer produced at all.
	res, err := e.Run(nil)
	if err != nil || len(res) != 1 || res[0].Status != StatusUpdate || res[0].Target.Path != path {
		t.Fatalf("results = %+v, err = %v", res, err)
	}
	if got := readFileT(t, path); got != "{\n  \"mine\": 1\n}\n" {
		t.Errorf("file = %q", got)
	}
	if m := readFileT(t, e.StatePath); strings.Contains(m, path) {
		t.Errorf("manifest still lists the path:\n%s", m)
	}
	// Idempotent: nothing left to retire.
	if res, err := e.Run(nil); err != nil || len(res) != 0 {
		t.Errorf("second run = %+v, %v", res, err)
	}
}

func TestRetireDeletesACreatedFileOnceEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "new.json")
	e := engineIn(dir)
	if res := runOne(t, e, Target{Path: path, Format: JSON, Managed: retireManaged()}); res.Err != nil || res.Status != StatusCreate {
		t.Fatalf("result = %+v", res)
	}
	res, err := e.Run(nil)
	if err != nil || len(res) != 1 || res[0].Status != StatusDelete {
		t.Fatalf("results = %+v, err = %v", res, err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("file still exists")
	}
}

func TestRetireKeepsACreatedFileTheUserAddedTo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "new.json")
	e := engineIn(dir)
	runOne(t, e, Target{Path: path, Format: JSON, Managed: retireManaged()})
	writeFileT(t, path, `{"theme": "x", "mine": true, "mcpServers": {"docs": {"url": "u"}}}`)
	res, err := e.Run(nil)
	if err != nil || len(res) != 1 || res[0].Status != StatusUpdate {
		t.Fatalf("results = %+v, err = %v", res, err)
	}
	if got := readFileT(t, path); got != `{"mine": true}` {
		t.Errorf("file = %q", got)
	}
}

func TestEmptyManagedSetRetiresLikeAnAbsentTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "new.json")
	e := engineIn(dir)
	runOne(t, e, Target{Path: path, Format: JSON, Managed: retireManaged()})
	res := runOne(t, e, Target{Path: path, Format: JSON, Managed: map[string]any{}})
	if res.Err != nil || res.Status != StatusDelete {
		t.Fatalf("result = %+v", res)
	}
	// And a target that has nothing and no file creates nothing.
	res = runOne(t, e, Target{Path: path, Format: JSON, Managed: map[string]any{}})
	if res.Err != nil || res.Status != StatusUnchanged {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("an empty target created a file")
	}
}

func TestRetireDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "new.json")
	e := engineIn(dir)
	runOne(t, e, Target{Path: path, Format: JSON, Managed: retireManaged()})
	e.DryRun = true
	res, err := e.Run(nil)
	if err != nil || len(res) != 1 || res[0].Status != StatusDelete || res[0].Diff == "" {
		t.Fatalf("results = %+v, err = %v", res, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("dry run deleted the file")
	}
}

func TestPartialRunLeavesOtherPathsAlone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "new.json")
	e := engineIn(dir)
	runOne(t, e, Target{Path: path, Format: JSON, Managed: retireManaged()})
	e.Partial = true
	if res, err := e.Run(nil); err != nil || len(res) != 0 {
		t.Fatalf("results = %+v, err = %v", res, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("a partial run retired a file")
	}
}

func TestRetireText(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (Engine, string) {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, "AGENTS.md")
		e := engineIn(dir)
		if res := runOne(t, e, Target{Path: path, Format: Text, Managed: "rules\n"}); res.Err != nil {
			t.Fatal(res.Err)
		}
		return e, path
	}

	t.Run("untouched file is deleted", func(t *testing.T) {
		t.Parallel()
		e, path := setup(t)
		res, err := e.Run(nil)
		if err != nil || len(res) != 1 || res[0].Status != StatusDelete {
			t.Fatalf("results = %+v, err = %v", res, err)
		}
		if _, err := os.Stat(path); err == nil {
			t.Error("file still exists")
		}
	})

	t.Run("edited file is reported as drift and left", func(t *testing.T) {
		t.Parallel()
		e, path := setup(t)
		writeFileT(t, path, "rules\nmine\n")
		res, err := e.Run(nil)
		if err != nil || len(res) != 1 || res[0].Status != StatusDrift || res[0].Err != nil {
			t.Fatalf("results = %+v, err = %v", res, err)
		}
		if got := readFileT(t, path); got != "rules\nmine\n" {
			t.Errorf("file = %q", got)
		}
		// Reported once: the manifest forgot it.
		if res, _ := e.Run(nil); len(res) != 0 {
			t.Errorf("second run = %+v", res)
		}
	})

	t.Run("a file already gone is forgotten quietly", func(t *testing.T) {
		t.Parallel()
		e, path := setup(t)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if res, err := e.Run(nil); err != nil || len(res) != 0 {
			t.Fatalf("results = %+v, err = %v", res, err)
		}
	})
}

func TestWriteFailsWhenTheFileChanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	writeFileT(t, path, `{"a": 1}`)
	tg := Target{Path: path, Format: JSON, Managed: map[string]any{"a": 2}}
	// The compare-and-swap check sees a file another program rewrote after the read.
	if err := writeFile(tg, []byte(`{"a": 2}`), `{"a": 1}`, true); err != nil {
		t.Fatalf("unchanged file: %v", err)
	}
	writeFileT(t, path, `{"a": 1, "other": true}`)
	err := writeFile(tg, []byte(`{"a": 3}`), `{"a": 1}`, true)
	if err == nil || !strings.Contains(err.Error(), "changed while agents was running") {
		t.Fatalf("err = %v", err)
	}
	if got := readFileT(t, path); got != `{"a": 1, "other": true}` {
		t.Errorf("file = %q", got)
	}
	// A file that appeared after a missing read also fails the write.
	late := filepath.Join(dir, "late.json")
	writeFileT(t, late, `{}`)
	if err := writeFile(Target{Path: late}, []byte(`{"a": 1}`), "", false); err == nil {
		t.Error("write over a file that appeared must fail")
	}
	entries, _ := os.ReadDir(dir)
	for _, en := range entries {
		if strings.Contains(en.Name(), ".tmp-") {
			t.Errorf("leftover temp file %s", en.Name())
		}
	}
}

func TestDiffOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	const live = "{\n  \"a\": 1\n}\n"
	tg := Target{Path: path, Format: JSON, Managed: map[string]any{"a": 2}}
	for _, tt := range []struct {
		name     string
		e        Engine
		wantDiff bool
	}{
		{"a real run builds none", Engine{}, false},
		{"Diff builds one", Engine{Diff: true}, true},
		{"a dry run builds one", Engine{DryRun: true}, true},
	} {
		writeFileT(t, path, live)
		res := runOne(t, tt.e, tg)
		if res.Err != nil || (res.Diff != "") != tt.wantDiff {
			t.Errorf("%s: result = %+v", tt.name, res)
		}
	}
}
