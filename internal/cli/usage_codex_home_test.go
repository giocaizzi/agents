package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/provider"
	usg "github.com/giocaizzi/agents/internal/usage"
)

func codexAuthFile(access, accountID string) string {
	return fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"access_token":%q,"account_id":%q}}`, access, accountID)
}

// fiveHourByHarness maps each row's first harness to its 5h used percent.
func fiveHourByHarness(t *testing.T, snaps []usg.Snapshot) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, s := range snaps {
		if s.Source != "codex-session" {
			out[s.Harnesses[0]] = -1
			continue
		}
		l := limitByName(t, s, "5h")
		if l.UsedPct == nil {
			t.Fatalf("row %v has no 5h percent", s.Harnesses)
		}
		out[s.Harnesses[0]] = *l.UsedPct
	}
	return out
}

func TestCodexSessionFallbackIsPerProfile(t *testing.T) {
	t.Parallel()
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)

	views := map[string]func(*testing.T, []provider.Account, usageFetcher) []usg.Snapshot{
		"cached view": func(_ *testing.T, accts []provider.Account, f usageFetcher) []usg.Snapshot {
			return cachedView(accts, nil, f)
		},
		"failed live": func(t *testing.T, accts []provider.Account, f usageFetcher) []usg.Snapshot {
			t.Helper()
			return liveSnapshots(t.Context(), accts, f, 5*time.Second)
		},
	}
	for name, view := range views {
		t.Run(name+"/CODEX_HOME selects the work profile", func(t *testing.T) {
			t.Parallel()
			f := testFetcher(t, failing)
			home := f.home
			store := fakeStore{home: home, env: map[string]string{"CODEX_HOME": filepath.Join(home, ".codex-work")}, files: map[string]string{
				filepath.Join(home, ".codex", "auth.json"):      codexAuthFile("fake-a1", "acct-1"),
				filepath.Join(home, ".codex-work", "auth.json"): codexAuthFile("fake-a2", "acct-2"),
			}}
			writeCodexRollout(t, filepath.Join(home, ".codex"), 11)
			writeCodexRollout(t, filepath.Join(home, ".codex-work"), 77)

			got := fiveHourByHarness(t, view(t, provider.Discover(store), f))

			if diff := cmp.Diff(map[string]float64{"codex": 11, "codex:work": 77}, got); diff != "" {
				t.Errorf("5h by row (-want +got):\n%s", diff)
			}
		})
		t.Run(name+"/only the work profile is logged in", func(t *testing.T) {
			t.Parallel()
			f := testFetcher(t, failing)
			home := f.home
			store := fakeStore{home: home, files: map[string]string{
				filepath.Join(home, ".codex-work", "auth.json"): codexAuthFile("fake-a2", "acct-2"),
			}}
			writeCodexRollout(t, filepath.Join(home, ".codex"), 11)
			writeCodexRollout(t, filepath.Join(home, ".codex-work"), 77)

			got := fiveHourByHarness(t, view(t, provider.Discover(store), f))

			if diff := cmp.Diff(map[string]float64{"codex": 77}, got); diff != "" {
				t.Errorf("5h by row (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSameNameProfilesBothAppearInCachedView(t *testing.T) {
	t.Parallel()
	store := fakeStore{
		home: "/h",
		env:  map[string]string{"CLAUDE_CONFIG_DIR": "/h/proj/.claude-work"},
		files: map[string]string{
			"/h/proj/.claude-work/.credentials.json": claudeCredsFile(tokA, "max"),
			"/h/.claude-work/.credentials.json":      claudeCredsFile(tokB, "max"),
		},
	}

	got := cachedView(provider.Discover(store), nil, testFetcher(t, nil))

	labels := make([]string, 0, len(got))
	for _, s := range got {
		labels = append(labels, s.Harnesses[0])
	}
	slices.Sort(labels)
	if diff := cmp.Diff([]string{"claude:h/work", "claude:proj/work"}, labels); diff != "" {
		t.Errorf("rows (-want +got):\n%s", diff)
	}
}
