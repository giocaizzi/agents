//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A FIFO in place of usage.json neither hangs the cache read nor the doctor.
func TestReadUsageCacheFIFODoesNotHang(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), usageCacheFile)
	mkfifo(t, path)
	done := make(chan error, 1)
	go func() {
		_, err := readUsageCache(path)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("readUsageCache accepted a FIFO")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readUsageCache blocked on a FIFO")
	}
}

func TestRunDoctorFIFOCacheDoesNotHang(t *testing.T) {
	t.Parallel()
	d, out, errb, path := doctorEnv(t, "models-fresh")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	mkfifo(t, path)
	f := testFetcher(t, nil)
	done := make(chan int, 1)
	go func() { done <- d.runDoctorWith(t.Context(), nil, fakeStore{home: f.home}, f) }()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit = %d, want 0", code)
		}
		if !strings.Contains(errb.String(), "ignoring usage cache") || !strings.Contains(out.String(), "(no usage cache; -live to refresh)") {
			t.Errorf("stdout %q, stderr %q", out, errb)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("doctor blocked on a FIFO cache")
	}
}
