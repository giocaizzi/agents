package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemovedCommandsFailLocally(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"statusline", []string{"statusline", "--help"}, "unknown command: statusline"},
		{"models render", []string{"models", "render", "--help"}, "unknown models subcommand: render"},
		{"sync doc", []string{"models", "sync", "-doc", "unused.md", "-file", filepath.Join(t.TempDir(), "missing.json")}, "flag provided but not defined: -doc"},
		{"help statusline", []string{"help", "statusline"}, "unknown help command: statusline"},
		{"help models render", []string{"help", "models", "render"}, "unknown help command: models render"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out, stderr bytes.Buffer
			d := helpDeps(&out, &stderr)
			if code := d.Run(t.Context(), tt.args); code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("code=%d stdout=%q stderr=%q; want local error %q", code, &out, &stderr, tt.want)
			}
		})
	}
}

func TestModelsSyncLeavesExistingDocsUntouched(t *testing.T) {
	t.Parallel()
	_, file, doc := repoDir(t, "models.json")
	contents := []byte("# Hand-maintained models\n")
	stamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.MkdirAll(filepath.Dir(doc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{doc, filepath.Dir(doc)} {
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	syncJSONOnly(t, file)
	if !bytes.Equal(mustRead(t, doc), contents) {
		t.Error("existing docs changed")
	}
	for _, path := range []string{doc, filepath.Dir(doc)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(stamp) {
			t.Errorf("%s touched: %v", path, info.ModTime())
		}
	}
}

func TestModelsSyncDoesNotCreateDocs(t *testing.T) {
	t.Parallel()
	root, file, _ := repoDir(t, "models.json")
	syncJSONOnly(t, file)
	if _, err := os.Stat(filepath.Join(root, "docs")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("docs directory created: %v", err)
	}
}

func syncJSONOnly(t *testing.T, file string) {
	t.Helper()
	before := mustRead(t, file)
	srv := newFakeServer(t)
	d, _, stderr := testDeps()
	if code := d.syncModels(t.Context(), modelsOpts{file: file}, srv.endpoints(), fixedNow); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if bytes.Equal(before, mustRead(t, file)) {
		t.Error("JSON was not refreshed")
	}
	entries, err := os.ReadDir(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "models.json" {
		t.Errorf("unexpected files beside JSON: %v", entries)
	}
}
