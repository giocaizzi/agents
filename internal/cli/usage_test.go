package cli

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/provider"
	usg "github.com/giocaizzi/agents/internal/usage"
)

// fakeStore is an in-memory provider.Store on Linux: files only, no keychain.
type fakeStore struct {
	home  string
	files map[string]string
	env   map[string]string
}

func (s fakeStore) ReadFile(path string) ([]byte, error) {
	if c, ok := s.files[path]; ok {
		return []byte(c), nil
	}
	return nil, fs.ErrNotExist
}
func (fakeStore) Secret(string, string) (string, error) { return "", fs.ErrNotExist }
func (s fakeStore) Exists(path string) bool             { _, ok := s.files[path]; return ok }
func (s fakeStore) Getenv(k string) string              { return s.env[k] }
func (fakeStore) Canonical(p string) string             { return p }
func (fakeStore) SameFile(a, b string) bool             { return a == b }
func (s fakeStore) ListDir(dir string) []string         { return listFiles(s.files, dir) }
func (s fakeStore) Home() string                        { return s.home }
func (fakeStore) GOOS() string                          { return "linux" }
func (fakeStore) Now() time.Time                        { return usageNow }
func (fakeStore) OpencodeCredentials(string) ([]provider.OpencodeRow, error) {
	return nil, fs.ErrNotExist
}

func usageDeps(t *testing.T) (Deps, *bytes.Buffer, *bytes.Buffer, string) {
	t.Helper()
	cacheHome := t.TempDir()
	var out, errb bytes.Buffer
	d := Deps{
		Stdout: &out, Stderr: &errb, GOOS: runtime.GOOS,
		Getenv: func(k string) string {
			if k == "XDG_CACHE_HOME" {
				return cacheHome
			}
			return ""
		},
	}
	return d, &out, &errb, filepath.Join(cacheHome, "chezmoi-agents", "usage.json")
}

func TestRunUsageFlagError(t *testing.T) {
	t.Parallel()
	d, _, errb, _ := usageDeps(t)
	if code := d.runUsageWith(t.Context(), []string{"-nope"}, fakeStore{}, testFetcher(t, nil)); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "flag parse error") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestRunUsageDispatch(t *testing.T) {
	t.Parallel()
	d, out, _, _ := usageDeps(t)
	if code := d.Run(t.Context(), []string{"usage", "-bogus"}); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	_ = out
}

func TestRunUsageCachedWithoutCache(t *testing.T) {
	t.Parallel()
	d, out, _, _ := usageDeps(t)
	f := testFetcher(t, nil)
	if code := d.runUsageWith(t.Context(), nil, fakeStore{home: f.home}, f); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"chezmoi agents usage", "no cache yet; -live to refresh"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestRunUsageLiveWritesCache(t *testing.T) {
	t.Parallel()
	d, out, errb, path := usageDeps(t)
	f := testFetcher(t, nil)
	code := d.runUsageWith(t.Context(), []string{"-live"}, fakeStore{home: f.home}, f)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, errb)
	}
	if !strings.Contains(out.String(), "(live)") {
		t.Errorf("output = %q", out)
	}
	c, err := readUsageCache(path)
	if err != nil {
		t.Fatalf("cache not readable: %v", err)
	}
	if !c.WrittenAt.Equal(usageNow) {
		t.Errorf("WrittenAt = %v, want %v", c.WrittenAt, usageNow)
	}
}

func TestUsageCacheRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sub", "usage.json")
	snaps := sampleSnapshots()

	if err := writeUsageCache(path, snaps, usageNow); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		fi, serr := os.Stat(path)
		if serr != nil {
			t.Fatal(serr)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("cache mode = %v, want 0600", fi.Mode().Perm())
		}
		di, _ := os.Stat(filepath.Dir(path))
		if di.Mode().Perm() != 0o700 {
			t.Errorf("cache dir mode = %v, want 0700", di.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
	got, err := readUsageCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.WrittenAt.Equal(usageNow) {
		t.Errorf("WrittenAt = %v", got.WrittenAt)
	}
	if diff := cmp.Diff(sortSnapshots(snaps), sortSnapshots(got.Snapshots)); diff != "" {
		t.Errorf("snapshots (-want +got):\n%s", diff)
	}
}

func TestReadUsageCacheMissingIsEmpty(t *testing.T) {
	t.Parallel()
	c, err := readUsageCache(filepath.Join(t.TempDir(), "usage.json"))
	if err != nil || len(c.Snapshots) != 0 {
		t.Errorf("= %+v, %v", c, err)
	}
}

func TestSnapKey(t *testing.T) {
	t.Parallel()
	a := snapKey("codex", "openai")
	if a == snapKey("pi:openai", "openai") || a == snapKey("codex", "anthropic") {
		t.Error("snapKey collides across distinct accounts")
	}
	if a != snapKey("codex", "openai") {
		t.Error("snapKey is not stable")
	}
}

func TestFilterProvider(t *testing.T) {
	t.Parallel()
	got := filterProvider(sampleSnapshots(), "anthropic")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	for _, s := range got {
		if s.Provider != "anthropic" {
			t.Errorf("provider %q kept", s.Provider)
		}
	}
	if len(filterProvider(sampleSnapshots(), "nope")) != 0 {
		t.Error("unknown provider kept rows")
	}
}

func TestCachedViewListsMissingAccounts(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, nil)
	cached := []usg.Snapshot{{Provider: "anthropic", Account: "me@example.com", Harnesses: []string{"claude"}, Status: usg.StatusOK}}
	accts := []provider.Account{
		{Provider: "anthropic", Label: "me@example.com", Harnesses: []string{"claude"}, Auth: provider.AuthOAuth},
		{Provider: "github-copilot", Label: "octocat", Harnesses: []string{"copilot"}, Auth: provider.AuthOAuth},
		{Provider: "openrouter", Label: "", Harnesses: []string{"pi"}, Auth: provider.AuthAPIKey, Status: usg.StatusNoAPI, Note: "no usage API"},
		{Provider: "openai", Label: "me@example.com", Harnesses: []string{"codex"}, Auth: provider.AuthOAuth, CodexHome: filepath.Join(f.home, ".codex")},
		{Provider: "google", Harnesses: []string{"antigravity"}, Auth: provider.AuthCLI},
	}

	got := cachedView(accts, cached, f)

	byProvider := map[string]usg.Snapshot{}
	for _, s := range got {
		byProvider[s.Provider] = s
	}
	if len(got) != 5 {
		t.Fatalf("rows = %d, want 5", len(got))
	}
	if s := byProvider["github-copilot"]; s.Status != usg.StatusUnavailable || s.Note != noDataNote {
		t.Errorf("copilot row = %+v", s)
	}
	if s := byProvider["openrouter"]; s.Status != usg.StatusNoAPI || s.Note != "no usage API" {
		t.Errorf("openrouter row = %+v", s)
	}
	// openai and google have no offline data here, so they ask for -live.
	for _, p := range []string{"openai", "google"} {
		if s := byProvider[p]; s.Status != usg.StatusUnavailable || s.Note != noDataNote {
			t.Errorf("%s row = %+v", p, s)
		}
	}
}

func TestCachedViewHarnessAddedKeepsOneRow(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, nil)
	cached := []usg.Snapshot{{ID: "claude", Provider: "anthropic", Account: "me@example.com", Harnesses: []string{"claude"}, Status: usg.StatusOK, Source: "api", Limits: []usg.Limit{{Name: "5h"}}}}
	accts := []provider.Account{{ID: "claude", Provider: "anthropic", Label: "me@example.com", Harnesses: []string{"claude", "pi"}, Auth: provider.AuthOAuth, IdentityKnown: true}}

	got := cachedView(accts, cached, f)

	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1: %+v", len(got), got)
	}
	if diff := cmp.Diff([]string{"claude", "pi"}, got[0].Harnesses); diff != "" {
		t.Errorf("harnesses (-want +got):\n%s", diff)
	}
	if got[0].Status != usg.StatusOK || len(got[0].Limits) != 1 {
		t.Errorf("cached data lost: %+v", got[0])
	}
}

func TestCachedViewDropsLoggedOutAccount(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, nil)
	cached := []usg.Snapshot{
		{ID: "claude", Provider: "anthropic", Account: "me@example.com", Harnesses: []string{"claude"}, Status: usg.StatusOK},
		{ID: "copilot", Provider: "github-copilot", Account: "octocat", Harnesses: []string{"copilot"}, Status: usg.StatusOK},
	}
	accts := []provider.Account{{ID: "claude", Provider: "anthropic", Label: "me@example.com", Harnesses: []string{"claude"}, Auth: provider.AuthOAuth}}

	got := cachedView(accts, cached, f)

	if len(got) != 1 || got[0].Provider != "anthropic" {
		t.Errorf("rows = %+v, want only anthropic", got)
	}
}

func TestCachedViewDuplicates(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, nil)
	snap := usg.Snapshot{ID: "claude", Provider: "anthropic", Account: "me@example.com", Harnesses: []string{"claude"}, Status: usg.StatusOK}
	acct := provider.Account{ID: "claude", Provider: "anthropic", Label: "me@example.com", Harnesses: []string{"claude"}, Auth: provider.AuthOAuth}

	if got := cachedView([]provider.Account{acct, acct}, []usg.Snapshot{snap, snap}, f); len(got) != 1 {
		t.Errorf("cached duplicates: rows = %d, want 1", len(got))
	}
	gh := provider.Account{ID: "copilot", Provider: "github-copilot", Label: "octocat", Harnesses: []string{"copilot"}, Auth: provider.AuthOAuth}
	if got := cachedView([]provider.Account{gh, gh}, nil, f); len(got) != 1 {
		t.Errorf("duplicate accounts without cache: rows = %d, want 1", len(got))
	}
}

func TestKeepStaleDropsUndiscoveredAndTakesHarnesses(t *testing.T) {
	t.Parallel()
	cached := []usg.Snapshot{
		{ID: "claude", Provider: "anthropic", Account: "a", Harnesses: []string{"claude"}, Status: usg.StatusOK, Source: "api"},
		{ID: "copilot", Provider: "github-copilot", Account: "gone", Harnesses: []string{"copilot"}, Status: usg.StatusOK},
	}
	live := []usg.Snapshot{{ID: "claude", Provider: "anthropic", Account: "a", Harnesses: []string{"claude", "pi"}, Status: usg.StatusUnavailable, Note: "HTTP 500"}}

	got := keepStale(live, cached)

	if len(got) != 1 || got[0].Status != usg.StatusStale {
		t.Fatalf("rows = %+v, want the one live account, stale", got)
	}
	if diff := cmp.Diff([]string{"claude", "pi"}, got[0].Harnesses); diff != "" {
		t.Errorf("harnesses (-want +got):\n%s", diff)
	}
}

func TestCachedViewUsesOfflineSources(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, nil)
	writeCodexSession(t, f.home)
	writeAgyLog(t, f.home, "cli-20261106_100000.log", "E1106 11:07:11.896026 2480 errorreport.go:224] RESOURCE_EXHAUSTED (code 429): quota. Resets in 4h58m35s.\n")
	accts := []provider.Account{
		{Provider: "openai", Label: "me@example.com", Harnesses: []string{"codex"}, Auth: provider.AuthOAuth, CodexHome: filepath.Join(f.home, ".codex")},
		{Provider: "google", Harnesses: []string{"antigravity"}, Auth: provider.AuthCLI},
	}

	got := cachedView(accts, nil, f)

	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2", len(got))
	}
	want := map[string]string{"openai": "codex-session", "google": "agy-log"}
	for _, s := range got {
		if s.Status != usg.StatusStale || s.Source != want[s.Provider] || len(s.Limits) == 0 || s.Account != map[string]string{"openai": "me@example.com"}[s.Provider] {
			t.Errorf("%s row = %+v", s.Provider, s)
		}
	}
}

func TestKeepStale(t *testing.T) {
	t.Parallel()
	old := usageNow.Add(-time.Hour)
	cached := []usg.Snapshot{
		{ID: "anthropic", Provider: "anthropic", Account: "a", Harnesses: []string{"claude"}, Status: usg.StatusOK, Source: "api", FetchedAt: old, Limits: []usg.Limit{{Name: "5h"}}},
		{ID: "openai", Provider: "openai", Account: "b", Harnesses: []string{"codex"}, Status: usg.StatusExpired, FetchedAt: old},
	}
	live := []usg.Snapshot{
		{ID: "anthropic", Provider: "anthropic", Account: "a", Harnesses: []string{"claude"}, Status: usg.StatusUnavailable, Note: "rate limited (429)"},
		{ID: "openai", Provider: "openai", Account: "b", Harnesses: []string{"codex"}, Status: usg.StatusUnavailable, Note: "HTTP 500"},
		{ID: "github-copilot", Provider: "github-copilot", Account: "c", Harnesses: []string{"copilot"}, Status: usg.StatusUnavailable, Note: "HTTP 500"},
		{Provider: "google", Account: "", Harnesses: []string{"antigravity"}, Status: usg.StatusOK},
	}

	got := keepStale(live, cached)

	if s := got[0]; s.Status != usg.StatusStale || s.Note != "rate limited (429)" || len(s.Limits) != 1 || !s.FetchedAt.Equal(old) || s.Source != "api" {
		t.Errorf("failed account did not keep its cached snapshot as stale: %+v", s)
	}
	if got[1].Status != usg.StatusUnavailable {
		t.Errorf("a non-ok cached snapshot must not replace the failure: %+v", got[1])
	}
	if got[2].Status != usg.StatusUnavailable || got[3].Status != usg.StatusOK {
		t.Errorf("accounts without cache changed: %+v / %+v", got[2], got[3])
	}
}

func TestRunUsageEndToEnd(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":31,"resets_at":"2026-11-06T14:14:00Z"}}`))
	}))
	defer srv.Close()
	f := testFetcher(t, srv)
	expires := usageNow.Add(time.Hour).UnixMilli()
	store := fakeStore{home: "/h", files: map[string]string{
		"/h/.claude/.credentials.json": fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d,"subscriptionType":"max","rateLimitTier":"default_claude_max_5x"}}`, secret, expires),
	}}
	d, out, errb, path := usageDeps(t)

	// Cached, before any live run: the account is listed without data.
	if code := d.runUsageWith(t.Context(), nil, store, f); code != 0 {
		t.Fatalf("cached exit = %d, stderr %q", code, errb)
	}
	if !strings.Contains(out.String(), "anthropic") || !strings.Contains(out.String(), noDataNote) {
		t.Errorf("cached output before -live:\n%s", out)
	}

	// Live: queried, shown and cached.
	out.Reset()
	if code := d.runUsageWith(t.Context(), []string{"-live"}, store, f); code != 0 {
		t.Fatalf("live exit = %d, stderr %q", code, errb)
	}
	for _, want := range []string{"(live)", "anthropic", "claude", "5h", " 31%", "resets in 2h 14m"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("live output lacks %q:\n%s", want, out)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cache not written: %v", err)
	}
	if strings.Contains(string(raw), secret) || strings.Contains(out.String(), secret) || strings.Contains(errb.String(), secret) {
		t.Error("the token reached the cache or the output")
	}

	// Cached again: served from the cache with no network, filtered by provider.
	srv.Close()
	out.Reset()
	if code := d.runUsageWith(t.Context(), []string{"-provider", "anthropic"}, store, f); code != 0 {
		t.Fatalf("cached exit = %d", code)
	}
	if !strings.Contains(out.String(), "cached ") || !strings.Contains(out.String(), " 31%") {
		t.Errorf("cached output after -live:\n%s", out)
	}
	out.Reset()
	if code := d.runUsageWith(t.Context(), []string{"-provider", "openai"}, store, f); code != 0 {
		t.Fatalf("filtered exit = %d", code)
	}
	if strings.Contains(out.String(), "anthropic") {
		t.Errorf("-provider openai still shows anthropic:\n%s", out)
	}

	// A later failing -live keeps the cached numbers, marked stale.
	out.Reset()
	if code := d.runUsageWith(t.Context(), []string{"-live"}, store, f); code != 0 {
		t.Fatalf("failing live exit = %d", code)
	}
	if !strings.Contains(out.String(), "(stale, api") || !strings.Contains(out.String(), " 31%") {
		t.Errorf("failed -live did not keep the cached snapshot as stale:\n%s", out)
	}
}

// listFiles returns the sorted first path components under dir.
func listFiles(files map[string]string, dir string) []string {
	seen := map[string]bool{}
	var out []string
	for f := range files {
		rest, ok := strings.CutPrefix(f, dir+string(filepath.Separator))
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, string(filepath.Separator))
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}
