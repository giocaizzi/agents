package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/giocaizzi/agents/internal/provider"
	usg "github.com/giocaizzi/agents/internal/usage"
)

const (
	maxBody        = 1 << 20 // 1 MiB
	userAgent      = "agents"
	liveTimeout    = 10 * time.Second
	agyUnavailable = "agy did not answer /usage"
	hdrAuth        = "Authorization"
	hdrAccept      = "Accept"
	jsonType       = "application/json"
	sourceAPI      = "api"
)

// fetcher queries the providers. Base URLs and the agy lookup are fields so
// tests can point them at httptest servers and a fake script.
type usageFetcher struct {
	client       *http.Client
	anthropicURL string
	chatgptURL   string
	githubURL    string
	agyPath      func() (string, error)
	home         string
	now          func() time.Time
	// memo holds the ChatGPT usage answers of this run, so a credential is
	// sent to the usage API at most once; nil (a test literal) memoizes nothing.
	memo *usageMemo
}

// usageMemo remembers the outcome, failures included, of one request per
// credential fingerprint. Concurrent callers with the same credential wait for
// the one request.
type usageMemo struct {
	mu      sync.Mutex
	entries map[string]*memoEntry
}

type memoEntry struct {
	once sync.Once
	body []byte
	err  error
}

func newUsageMemo() *usageMemo { return &usageMemo{entries: map[string]*memoEntry{}} }

func (m *usageMemo) do(key string, fn func() ([]byte, error)) ([]byte, error) {
	if m == nil || key == "" {
		return fn()
	}
	m.mu.Lock()
	e, ok := m.entries[key]
	if !ok {
		e = &memoEntry{}
		m.entries[key] = e
	}
	m.mu.Unlock()
	e.once.Do(func() { e.body, e.err = fn() })
	return e.body, e.err
}

func newFetcher(home string) usageFetcher {
	return usageFetcher{
		client: &http.Client{
			Timeout: liveTimeout,
			// No endpoint here redirects; following one could forward the token or
			// ChatGPT-Account-Id to another scheme or host.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		anthropicURL: "https://api.anthropic.com",
		chatgptURL:   "https://chatgpt.com",
		githubURL:    "https://api.github.com",
		agyPath:      func() (string, error) { return exec.LookPath("agy") },
		home:         home,
		now:          time.Now,
		memo:         newUsageMemo(),
	}
}

// statusError is an HTTP answer outside 2xx. It carries the code only: never a
// header or a body, which could echo a token.
type usageStatusError struct{ code int }

func (e usageStatusError) Error() string { return "HTTP " + strconv.Itoa(e.code) }

// failNote turns a request error into the note shown for the account. It never
// includes a URL query, a header or a body.
func failNote(err error) string {
	var se usageStatusError
	switch {
	case errors.As(err, &se):
		switch se.code {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "unauthorized (" + strconv.Itoa(se.code) + ")"
		case http.StatusTooManyRequests:
			return "rate limited (429)"
		}
		return se.Error()
	case errors.Is(err, context.DeadlineExceeded):
		return "unavailable: timeout"
	}
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return "unavailable: " + ue.Err.Error()
	}
	return "unavailable: " + err.Error()
}

func (f usageFetcher) base(acct provider.Account) usg.Snapshot {
	return usg.Snapshot{
		ID:        acct.ID,
		Provider:  acct.Provider,
		Account:   acct.Label,
		Harnesses: acct.Harnesses,
		Plan:      acct.Plan,
		Status:    usg.StatusOK,
		Note:      okNote(acct),
		FetchedAt: f.now(),

		Fingerprints:    acct.Fingerprints,
		IdentityUnknown: identityUnknown(acct),
	}
}

// okNote is the account's note when its status is OK: a fact about the
// identity that the row shows.
func okNote(acct provider.Account) string {
	if acct.Status == "" || acct.Status == usg.StatusOK {
		return acct.Note
	}
	return ""
}

func unavailable(s usg.Snapshot, note string) usg.Snapshot {
	s.Status = usg.StatusUnavailable
	s.Note = note
	s.Limits = nil
	return s
}

// fetch queries one account. It never returns an error: a failure is the
// snapshot's status.
func (f usageFetcher) fetch(ctx context.Context, acct provider.Account, timeout time.Duration) usg.Snapshot {
	snap := f.base(acct)
	if acct.Status != "" && acct.Status != usg.StatusOK {
		snap.Status = acct.Status
		snap.Note = acct.Note
		return snap
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch {
	case acct.Provider == provider.Google && acct.Auth == provider.AuthCLI:
		return f.fetchAgy(ctx, snap, timeout)
	case acct.Auth == provider.AuthOAuth && acct.Provider == provider.Anthropic:
		return f.fetchAnthropic(ctx, acct, snap)
	case acct.Auth == provider.AuthOAuth && acct.Provider == provider.OpenAI:
		return f.fetchOpenAI(ctx, acct, snap)
	case acct.Auth == provider.AuthOAuth && acct.Provider == provider.GitHubCopilot:
		return f.fetchCopilot(ctx, acct, snap)
	}
	snap.Status = usg.StatusNoAPI
	snap.Note = "no usage API"
	return snap
}

// get performs one GET and returns the body (at most 1 MiB). A non-2xx answer
// is a statusError.
func (f usageFetcher) get(ctx context.Context, rawURL string, header map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("bad request")
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		return nil, usageStatusError{resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}

func (f usageFetcher) fetchAnthropic(ctx context.Context, acct provider.Account, snap usg.Snapshot) usg.Snapshot {
	body, err := f.get(ctx, f.anthropicURL+"/api/oauth/usage", map[string]string{
		hdrAuth:          "Bearer " + acct.Cred.Token(),
		"anthropic-beta": "oauth-2025-04-20",
		hdrAccept:        jsonType,
	})
	if err != nil {
		return unavailable(snap, failNote(err))
	}
	limits, err := usg.ParseAnthropic(body, acct.Plan)
	if err != nil {
		return unavailable(snap, "unavailable: "+err.Error())
	}
	snap.Limits = limits
	snap.Source = sourceAPI
	return snap
}

// openaiHeader is the header of a ChatGPT usage request: the bearer token and,
// when the credential carries one, the account it belongs to.
func openaiHeader(acct provider.Account) map[string]string {
	header := map[string]string{
		hdrAuth:      "Bearer " + acct.Cred.Token(),
		"originator": "codex_cli_rs",
		hdrAccept:    jsonType,
	}
	if id := acct.Cred.AccountID(); id != "" {
		header["ChatGPT-Account-Id"] = id
	}
	return header
}

// openaiUsage is GET chatgpt.com/backend-api/wham/usage for the credential,
// sent at most once per run: the identity lookup and the usage fetch share the
// answer, failures included.
func (f usageFetcher) openaiUsage(ctx context.Context, acct provider.Account) ([]byte, error) {
	return f.memo.do(acct.Cred.Fingerprint(provider.OpenAI), func() ([]byte, error) {
		return f.get(ctx, f.chatgptURL+"/backend-api/wham/usage", openaiHeader(acct))
	})
}

func (f usageFetcher) fetchOpenAI(ctx context.Context, acct provider.Account, snap usg.Snapshot) usg.Snapshot {
	var (
		plan   usg.Plan
		limits []usg.Limit
		note   string
	)
	body, err := f.openaiUsage(ctx, acct)
	if err != nil {
		note = failNote(err)
	} else if plan, limits, err = usg.ParseOpenAI(body); err != nil {
		note = "unavailable: " + err.Error()
	}
	if note != "" {
		if fb, ok := f.codexSessionFallback(acct); ok {
			fb.Note = note
			return fb
		}
		return unavailable(snap, note)
	}
	if plan.Kind != usg.KindUnknown && plan.Kind != "" {
		snap.Plan = plan
	}
	snap.Limits = limits
	snap.Source = sourceAPI
	return snap
}

// githubHost is the only host a Copilot token is sent to (githubURL).
const githubHost = "https://github.com"

func (f usageFetcher) fetchCopilot(ctx context.Context, acct provider.Account, snap usg.Snapshot) usg.Snapshot {
	// The provider always sets Host for Copilot accounts; an empty or any
	// other host is never sent a token.
	host := acct.Host
	if host != githubHost {
		snap.Status = usg.StatusNotVisible
		snap.Note = hostNote(host)
		return snap
	}
	// The Copilot clients authenticate copilot_internal calls with
	// "Authorization: token <github token>", see
	// https://github.com/microsoft/vscode-copilot-chat/blob/main/src/platform/authentication/node/copilotTokenManager.ts
	auth := "token " + acct.Cred.Token()
	body, err := f.get(ctx, f.githubURL+"/copilot_internal/user", map[string]string{
		hdrAuth:   auth,
		hdrAccept: jsonType,
	})
	if err != nil {
		return unavailable(snap, failNote(err))
	}
	plan, limits, err := usg.ParseCopilotUser(body)
	if err != nil {
		return unavailable(snap, "unavailable: "+err.Error())
	}
	if plan.Kind != usg.KindUnknown && plan.Kind != "" {
		snap.Plan = plan
	}
	snap.Limits = limits
	snap.Source = sourceAPI

	if acct.Login != "" {
		billing, err := f.get(ctx, f.githubURL+"/users/"+url.PathEscape(acct.Login)+"/settings/billing/premium_request/usage", map[string]string{
			hdrAuth:                auth,
			hdrAccept:              "application/vnd.github+json",
			"X-GitHub-Api-Version": "2026-03-10",
		})
		var se usageStatusError
		switch {
		case err == nil:
			if l, perr := usg.ParseCopilotBilling(billing); perr == nil {
				snap.Limits = append(snap.Limits, l)
			}
		case errors.As(err, &se) && (se.code == http.StatusForbidden || se.code == http.StatusNotFound):
			snap.Limits = append(snap.Limits, hiddenSpend(snap, limits))
		}
	}
	return snap
}

// hostNote explains why a Copilot account's host is not queried.
func hostNote(host string) string {
	if host == "" {
		return "unknown GitHub host: usage not queried"
	}
	h := strings.ToLower(stripScheme(host))
	if name, _, _ := strings.Cut(h, ":"); name == "github.com" || name == "www.github.com" {
		return "unrecognised GitHub host form: usage not queried"
	}
	return "GitHub Enterprise host " + hostOnly(host) + ": usage not queried"
}

// hostOnly drops the scheme and any userinfo, which must never be printed.
func hostOnly(host string) string {
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		return u.Host
	}
	h := stripScheme(host)
	if i := strings.IndexByte(h, '/'); i >= 0 {
		h = h[:i]
	}
	if i := strings.LastIndexByte(h, '@'); i >= 0 {
		h = h[i+1:]
	}
	return h
}

func stripScheme(host string) string {
	if _, rest, ok := strings.Cut(host, "://"); ok {
		return rest
	}
	return host
}

// hiddenSpend is the spend row for a billing 403/404. "Managed by org" only
// when an organization pays or the premium limit is pooled; otherwise the
// token most likely lacks billing access.
func hiddenSpend(snap usg.Snapshot, limits []usg.Limit) usg.Limit {
	org := snap.Plan.Payer == usg.PayerOrg
	for _, l := range limits {
		if l.Scope == usg.ScopePool {
			org = true
		}
	}
	note := "not visible (token lacks billing access)"
	if org {
		note = "managed by org"
	}
	return usg.Limit{Name: "spend", Kind: usg.LimitSpend, Hidden: true, Note: note}
}

func (f usageFetcher) fetchAgy(ctx context.Context, snap usg.Snapshot, timeout time.Duration) usg.Snapshot {
	if body, err := f.runAgy(ctx, timeout); err == nil {
		if limits, perr := usg.ParseAgyUsage(body); perr == nil {
			snap.Limits = limits
			snap.Source = "agy"
			return snap
		}
	}
	acct := provider.Account{ID: snap.ID, Provider: snap.Provider, Label: snap.Account, Harnesses: snap.Harnesses, Plan: snap.Plan}
	if fb, ok := f.agyLogFallback(acct); ok {
		return fb
	}
	return unavailable(snap, agyUnavailable)
}

func (f usageFetcher) runAgy(ctx context.Context, timeout time.Duration) ([]byte, error) {
	path, err := f.agyPath()
	if err != nil {
		return nil, err
	}
	secs := max(int(timeout/time.Second), 1)
	cmd := exec.CommandContext(ctx, path, "-p", "/usage", "--output-format", "json", "--print-timeout", strconv.Itoa(secs)+"s")
	// A fresh private directory: a shared temp dir could hold planted workspace config.
	dir, err := os.MkdirTemp("", "agents-agy-")
	if err != nil {
		return nil, errors.New("agy failed")
	}
	defer os.RemoveAll(dir)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &limitWriter{w: &out, n: maxBody}
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		return nil, errors.New("agy failed")
	}
	return out.Bytes(), nil
}

// limitWriter drops everything past n bytes.
type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		_, _ = l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}

// agyLogFallback reads the newest Antigravity CLI log for an exhausted window
// that has not reset yet.
func (f usageFetcher) agyLogFallback(acct provider.Account) (usg.Snapshot, bool) {
	dir := filepath.Join(f.home, ".gemini", "antigravity-cli", "log")
	names, err := filepath.Glob(filepath.Join(dir, "cli-*.log"))
	if err != nil || len(names) == 0 {
		return usg.Snapshot{}, false
	}
	sort.Strings(names)
	newest := names[len(names)-1]
	stamp := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(newest), "cli-"), ".log")
	start, err := time.ParseInLocation("20060102_150405", stamp, f.now().Location())
	if err != nil {
		return usg.Snapshot{}, false
	}
	file, err := os.Open(filepath.Clean(newest))
	if err != nil {
		return usg.Snapshot{}, false
	}
	defer file.Close()
	limits, at, err := usg.ParseAgyLog(file, start)
	if err != nil {
		return usg.Snapshot{}, false
	}
	now := f.now()
	var keep []usg.Limit
	for _, l := range limits {
		if l.ResetsAt != nil && l.ResetsAt.After(now) {
			keep = append(keep, l)
		}
	}
	if len(keep) == 0 {
		return usg.Snapshot{}, false
	}
	snap := f.base(acct)
	snap.Limits = keep
	snap.Status = usg.StatusStale
	snap.Source = "agy-log"
	if !at.IsZero() {
		snap.FetchedAt = at
	}
	return snap, true
}

// codexSessionFallback reads the newest rate-limit snapshot Codex wrote in the
// session logs of the profile the account was logged in from, looking at the
// last 7 day directories. An account with no Codex profile has none.
func (f usageFetcher) codexSessionFallback(acct provider.Account) (usg.Snapshot, bool) {
	if acct.CodexHome == "" {
		return usg.Snapshot{}, false
	}
	root := filepath.Join(acct.CodexHome, "sessions")

	for _, day := range recentDays(root, 7) {
		for _, name := range rolloutFiles(day) {
			if snap, ok := f.codexSnapshot(acct, name); ok {
				return snap, true
			}
		}
	}
	return usg.Snapshot{}, false
}

// recentDays lists the newest n YYYY/MM/DD directories under root, newest first.
func recentDays(root string, n int) []string {
	var days []string
	for _, y := range sortedDirs(root) {
		for _, m := range sortedDirs(y) {
			for _, d := range sortedDirs(m) {
				if days = append(days, d); len(days) == n {
					return days
				}
			}
		}
	}
	return days
}

// rolloutFiles lists the rollout-*.jsonl entries of a day directory, newest
// (lexically last) first. Names are matched on the entry, never as a glob, so
// metacharacters in the path are plain characters.
func rolloutFiles(day string) []string {
	entries, err := os.ReadDir(day)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if n := e.Name(); strings.HasPrefix(n, "rollout-") && strings.HasSuffix(n, ".jsonl") {
			out = append(out, filepath.Join(day, n))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

// codexSnapshot parses one rollout log. A FIFO, a device or a file over the
// read cap is skipped.
func (f usageFetcher) codexSnapshot(acct provider.Account, name string) (usg.Snapshot, bool) {
	b, err := readRegular(name)
	if err != nil {
		return usg.Snapshot{}, false
	}
	plan, limits, at, err := usg.ParseCodexSession(bytes.NewReader(b))
	if err != nil || len(limits) == 0 {
		return usg.Snapshot{}, false
	}
	snap := f.base(acct)
	if plan.Kind != usg.KindUnknown && plan.Kind != "" {
		snap.Plan = plan
	}
	snap.Limits = limits
	snap.Status = usg.StatusStale
	snap.Source = "codex-session"
	if !at.IsZero() {
		snap.FetchedAt = at
	}
	return snap, true
}

// sortedDirs lists the subdirectories of dir, newest (lexically last) first.
func sortedDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

// liveSnapshots fetches every account in parallel, perProvider each, and
// returns the snapshots in the order of accounts.
func liveSnapshots(ctx context.Context, accounts []provider.Account, f usageFetcher, perProvider time.Duration) []usg.Snapshot {
	out := make([]usg.Snapshot, len(accounts))
	var wg sync.WaitGroup
	for i, acct := range accounts {
		wg.Go(func() { out[i] = f.fetch(ctx, acct, perProvider) })
	}
	wg.Wait()
	return out
}
