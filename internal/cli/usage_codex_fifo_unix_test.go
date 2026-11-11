//go:build !windows

package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giocaizzi/agents/internal/provider"
)

// A FIFO rollout in a listed ~/.codex-x profile never blocks a failed live fetch, and
// the regular rollout behind it is still used.
func TestLiveFallbackSkipsFIFORollout(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, failingServer(t))
	home := f.home
	codexHome := filepath.Join(home, ".codex-x")
	writeCodexRollout(t, codexHome, 33)
	day := filepath.Join(codexHome, "sessions", "2026", "11", "05")
	mkfifo(t, filepath.Join(day, "rollout-2026-11-05T23-00-00-fifo.jsonl")) // sorts first
	store := fakeStore{home: home, files: map[string]string{
		filepath.Join(codexHome, "auth.json"): codexAuthFile("fake-a", "acct-1"),
	}}
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}

	done := make(chan map[string]float64, 1)
	go func() {
		done <- fiveHourByHarness(t, liveSnapshots(t.Context(), provider.Discover(store), f, 5*time.Second))
	}()
	select {
	case got := <-done:
		if got["codex"] != 33 {
			t.Errorf("5h by row = %v, want codex 33 from the regular rollout", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("live fallback blocked on a FIFO rollout")
	}
}
