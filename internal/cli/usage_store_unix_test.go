//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/provider"
)

func mkfifo(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
}

func TestOSStoreReadFileNeverOpensAFIFO(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	fifo := filepath.Join(s.Home(), "auth.json")
	mkfifo(t, fifo)

	done := make(chan error, 1)
	go func() {
		_, err := s.ReadFile(fifo)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("ReadFile of a FIFO succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadFile of a FIFO blocked")
	}
}

func TestOSStoreListDirWithGlobMetacharacters(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	dir := filepath.Join(s.Home(), "[h*me]?")
	for _, d := range []string{".claude-b", ".claude-a"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if diff := cmp.Diff([]string{".claude-a", ".claude-b"}, s.ListDir(dir)); diff != "" {
		t.Errorf("ListDir (-want +got):\n%s", diff)
	}
}

// A FIFO named like a credential file is skipped silently in a listed profile
// and shown as unreadable in the default one, without blocking.
func TestDiscoverWithFIFOCredentialFiles(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	s.home = filepath.Join(s.home, "[u*]") // glob metacharacters in the home
	for _, d := range []string{".claude", ".claude-work"} {
		if err := os.MkdirAll(filepath.Join(s.home, d), 0o700); err != nil {
			t.Fatal(err)
		}
		mkfifo(t, filepath.Join(s.home, d, ".credentials.json"))
	}

	done := make(chan []provider.Account, 1)
	go func() { done <- provider.Discover(s) }()
	select {
	case got := <-done:
		if len(got) != 1 || got[0].Note != "credentials unreadable" || got[0].Harnesses[0] != "claude" {
			t.Errorf("accounts = %+v, want only the default dir as unreadable", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Discover blocked on a FIFO")
	}
}
