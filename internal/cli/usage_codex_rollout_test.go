package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/giocaizzi/agents/internal/provider"
)

func TestCodexFallbackWithGlobMetacharactersInHome(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("* and ? are not valid in Windows file names")
	}
	f := testFetcher(t, nil)
	home := filepath.Join(f.home, "[h*me]?")
	writeCodexRollout(t, filepath.Join(home, ".codex"), 42)
	acct := oauthAccount(provider.OpenAI)
	acct.CodexHome = filepath.Join(home, ".codex")
	snap, ok := f.codexSessionFallback(acct)
	if !ok || len(snap.Limits) == 0 {
		t.Fatalf("fallback found no rollout under a home with glob metacharacters: %+v", snap)
	}
}

func TestCodexFallbackSkipsOversizeRollout(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, nil)
	codexHome := filepath.Join(f.home, ".codex")
	day := filepath.Join(codexHome, "sessions", "2026", "11", "05")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	sparse(t, filepath.Join(day, "rollout-2026-11-05T10-00-00-big.jsonl"), maxReadFile+1)
	acct := oauthAccount(provider.OpenAI)
	acct.CodexHome = codexHome
	if _, ok := f.codexSessionFallback(acct); ok {
		t.Error("an oversize rollout must be skipped")
	}
}
