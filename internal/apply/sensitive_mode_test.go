package apply

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestWriteFileTightensSensitiveMode: a target carrying secrets ends up 0600
// even when the existing file was wider; one without keeps its mode.
func TestWriteFileTightensSensitiveMode(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	for _, tc := range []struct {
		name      string
		sensitive []string
		want      os.FileMode
	}{
		{"sensitive", []string{"s3cret"}, 0o600},
		{"plain", nil, 0o644},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "s.json")
			writeFileT(t, path, "{}\n")
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
			res := runOne(t, engineIn(dir), Target{
				Path: path, Format: JSON,
				Managed: map[string]any{"k": "s3cret"}, Sensitive: tc.sensitive,
			})
			if res.Err != nil {
				t.Fatal(res.Err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != tc.want {
				t.Errorf("mode = %o, want %o", got, tc.want)
			}
		})
	}
}
