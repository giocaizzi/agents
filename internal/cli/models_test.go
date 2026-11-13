package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/giocaizzi/agents/internal/catalog"
	"github.com/giocaizzi/agents/internal/config"
)

// arenaTotals is the row count per board of the fake dataset: text needs two pages.
var arenaTotals = map[string]int{"text_style_control": 150, "webdev": 20, "agent": 5}

var fixedNow = func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }

// fakeServer serves the three sources and records every request.
type fakeServer struct {
	*httptest.Server

	mu       sync.Mutex
	requests []string       // "path?query" in arrival order
	failures map[string]int // path -> remaining 500 answers
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	modelsDev, err := os.ReadFile(filepath.Join("testdata", "models", "modelsdev.json"))
	if err != nil {
		t.Fatal(err)
	}
	fs := &fakeServer{failures: map[string]int{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/api.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(modelsDev)
	})
	mux.HandleFunc("/rows", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		board := q.Get("config")
		total := arenaTotals[board]
		offset, _ := strconv.Atoi(q.Get("offset"))
		length, _ := strconv.Atoi(q.Get("length"))
		type rowEntry struct {
			RowIdx int            `json:"row_idx"`
			Row    map[string]any `json:"row"`
		}
		var rows []rowEntry
		for i := offset; i < min(offset+length, total); i++ {
			row := map[string]any{
				"model_name":               "Claude Opus 5.5 (High)",
				"organization":             "anthropic",
				"license":                  "proprietary",
				"rating":                   1500.0 - float64(i),
				"rating_lower":             1490.0,
				"rating_upper":             1510.0,
				"variance":                 1.0,
				"vote_count":               1000 + i,
				"rank":                     i%3 + 1,
				"category":                 "overall",
				"leaderboard_publish_date": "2026-11-05",
			}
			if board == "agent" {
				delete(row, "rating")
				delete(row, "vote_count")
				row["score"] = 0.8
				row["session_count"] = 50 + i
			}
			rows = append(rows, rowEntry{RowIdx: i, Row: row})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rows":           rows,
			"num_rows_total": total,
		})
	})
	mux.HandleFunc("/epoch.zip", func(w http.ResponseWriter, _ *http.Request) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for name, body := range map[string]string{
			"epoch_capabilities_index/eci_scores.csv": "Model,eci\nClaude Opus 5.5,155.5\n",
			"terminalbench_external.csv":              "Model version,Accuracy mean\nclaude-opus-5-5_max,0.61\n",
			"swe_bench_verified.csv":                  "Model version,mean_score\nclaude-opus-5-5_max,0.8\n",
			"metr_time_horizons_external.csv":         "Model version,Time horizon\nclaude-opus-5-5_max,240\n",
			"osworld_2_external.csv":                  "Model version,Binary accuracy\nclaude-opus-5-5_max,0.7\n",
		} {
			f, _ := zw.Create(name)
			_, _ = f.Write([]byte(body))
		}
		_ = zw.Close()
		_, _ = w.Write(buf.Bytes())
	})

	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		fs.requests = append(fs.requests, r.URL.RequestURI())
		fail := fs.failures[r.URL.Path] > 0
		if fail {
			fs.failures[r.URL.Path]--
		}
		fs.mu.Unlock()
		if r.Header.Get("User-Agent") != "agents" {
			http.Error(w, "bad user agent", http.StatusForbidden)
			return
		}
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *fakeServer) endpoints() endpoints {
	return endpoints{
		modelsDev: fs.URL + "/api.json",
		arenaRows: fs.URL + "/rows",
		epoch:     fs.URL + "/epoch.zip",
		backoff:   []time.Duration{0, 0},
	}
}

func (fs *fakeServer) failFirst(path string, n int) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.failures[path] = n
}

func (fs *fakeServer) log() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return slices.Clone(fs.requests)
}

// count is the number of requests whose path is path.
func (fs *fakeServer) count(path string) int {
	n := 0
	for _, r := range fs.log() {
		if strings.HasPrefix(r, path+"?") || r == path {
			n++
		}
	}
	return n
}

// arenaRequest is the set of "board offset" pairs requested from /rows.
func (fs *fakeServer) arenaRequests(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, r := range fs.log() {
		if !strings.HasPrefix(r, "/rows?") {
			continue
		}
		u, err := parseQuery(r)
		if err != nil {
			t.Fatal(err)
		}
		if u.Get("dataset") != "lmarena-ai/leaderboard-dataset" || u.Get("split") != "latest" || u.Get("length") != "100" {
			t.Errorf("unexpected arena query %q", r)
		}
		out = append(out, u.Get("config")+" "+u.Get("offset"))
	}
	slices.Sort(out)
	return out
}

func testDeps() (Deps, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	return Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: func(k string) string {
			switch k {
			case "NO_COLOR":
				return "1"
			case "HOME", "USERPROFILE": // no user config, nothing written
				return filepath.Join(string(filepath.Separator), "nonexistent", "agents-test-home")
			}
			return ""
		},
	}, &stdout, &stderr
}

// hermeticEnv is a Deps whose home, config and XDG directories live in a temp
// dir; cfgTOML, when set, is the config file ($AGENTS_CONFIG). It returns the
// resolved directories.
func hermeticEnv(t *testing.T, cfgTOML string) (Deps, *bytes.Buffer, *bytes.Buffer, config.Dirs) {
	t.Helper()
	home := t.TempDir()
	env := map[string]string{
		"HOME": home, "USERPROFILE": home, "NO_COLOR": "1",
		"XDG_CONFIG_HOME": filepath.Join(home, "config"),
		"XDG_CACHE_HOME":  filepath.Join(home, "cache"),
		"XDG_STATE_HOME":  filepath.Join(home, "state"),
		"XDG_DATA_HOME":   filepath.Join(home, "data"),
	}
	if cfgTOML != "" {
		path := filepath.Join(home, "agents.toml")
		if err := os.WriteFile(path, []byte(cfgTOML), 0o600); err != nil {
			t.Fatal(err)
		}
		env["AGENTS_CONFIG"] = path
	}
	var stdout, stderr bytes.Buffer
	d := Deps{Stdout: &stdout, Stderr: &stderr, GOOS: runtime.GOOS, Getenv: func(k string) string { return env[k] }}
	return d, &stdout, &stderr, config.ResolveDirs(d.Getenv, d.GOOS, home)
}

// plainOpts are modelsOpts for file with an empty resolved config.
func plainOpts(t *testing.T, file string) modelsOpts {
	t.Helper()
	return modelsOpts{file: file, cfg: &config.Resolved{Dirs: config.Dirs{Cache: t.TempDir()}}}
}

// repoDir copies a models.json fixture into a temp dir and returns the dir,
// the file and a doc path that must stay absent.
func repoDir(t *testing.T, fixture string) (root, file, doc string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "models", fixture))
	if err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	file = filepath.Join(root, "cache", modelsFile)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, file, filepath.Join(root, "docs", "MODELS.md")
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestModelsRouting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("help exits 0 with usage", func(t *testing.T) {
		t.Parallel()
		d, stdout, _ := testDeps()
		if code := d.Run(ctx, []string{"models", "help"}); code != 0 {
			t.Fatalf("exit %d, want 0", code)
		}
		for _, want := range []string{"agents models", "COMMANDS", "sync", "Without a subcommand, displays the model tier list"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("usage lacks %q", want)
			}
		}
	})

	t.Run("unknown subcommand exits 2", func(t *testing.T) {
		t.Parallel()
		d, _, stderr := testDeps()
		if code := d.Run(ctx, []string{"models", "bogus"}); code != 2 {
			t.Fatalf("exit %d, want 2", code)
		}
		if !strings.Contains(stderr.String(), "unknown models subcommand: bogus") {
			t.Errorf("stderr: %q", stderr.String())
		}
	})

	t.Run("bad flag exits 2", func(t *testing.T) {
		t.Parallel()
		d, _, _ := testDeps()
		if code := d.Run(ctx, []string{"models", "sync", "-nope"}); code != 2 {
			t.Fatalf("exit %d, want 2", code)
		}
	})
}

// placeCache puts a models fixture in the cache dir; "" puts nothing.
func placeCache(t *testing.T, dirs config.Dirs, fixture string) {
	t.Helper()
	if fixture == "" {
		return
	}
	if err := os.MkdirAll(dirs.Cache, 0o750); err != nil {
		t.Fatal(err)
	}
	data := mustRead(t, filepath.Join("testdata", "models", fixture))
	if err := os.WriteFile(filepath.Join(dirs.Cache, modelsFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// fileArgs is the -file flag for a fixture: none for "", a missing file for "none".
func fileArgs(t *testing.T, fixture string) []string {
	t.Helper()
	switch fixture {
	case "":
		return nil
	case "none":
		return []string{"-file", filepath.Join(t.TempDir(), "none.json")}
	}
	_, file, _ := repoDir(t, fixture)
	return []string{"-file", file}
}

func TestModelsShow(t *testing.T) {
	t.Parallel()
	const opus = "Claude Opus 5.5"
	tests := []struct {
		name       string
		config     string
		cacheFile  string // fixture placed in the cache dir
		fileFlag   string // fixture passed with -file; "none" is a missing file
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "-file prints the tiers", fileFlag: "models.json", wantStdout: opus},
		{
			name: "cache dir models.json is the default", config: "[models.tiers.fast]\nclaude = \"claude-haiku-5\"\n",
			cacheFile: "models.json", wantStdout: opus,
		},
		{name: "-file wins over the cache dir", cacheFile: "models-invalid.json", fileFlag: "models.json", wantStdout: opus},
		{name: "invalid file lists the errors", fileFlag: "models-invalid.json", wantCode: 1, wantStderr: "anthropic/does-not-exist"},
		{name: "missing -file", fileFlag: "none", wantCode: 1, wantStderr: "error:"},
		{name: "no cache file and no providers", wantCode: 1, wantStderr: "models.catalog.providers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, stdout, stderr, dirs := hermeticEnv(t, tt.config)
			placeCache(t, dirs, tt.cacheFile)
			args := append([]string{"models"}, fileArgs(t, tt.fileFlag)...)
			if code := d.Run(t.Context(), args); code != tt.wantCode {
				t.Fatalf("exit %d, want %d; stderr %q", code, tt.wantCode, stderr)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout lacks %q: %q", tt.wantStdout, stdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr lacks %q: %q", tt.wantStderr, stderr)
			}
			if tt.wantCode != 0 && stdout.Len() != 0 {
				t.Errorf("stdout not empty on failure: %q", stdout)
			}
		})
	}
}

func TestModelsLoadCatalogFromConfig(t *testing.T) {
	t.Parallel()
	d, _, _, dirs := hermeticEnv(t, `
[models.catalog]
providers = ["anthropic", "openai"]
since = "2026-02-01"
[models.catalog.pins]
"anthropic/claude-3-5-haiku" = "lightweight"
`)
	r, err := d.loadConfig(&configFlags{})
	if err != nil {
		t.Fatal(err)
	}
	cat, path, err := d.loadCatalog(r, "", false)
	if err != nil {
		t.Fatalf("loadCatalog failed: %v", err)
	}
	if !slices.Equal(cat.Providers, []string{"anthropic", "openai"}) {
		t.Errorf("got providers %v", cat.Providers)
	}
	if cat.Since != "2026-02-01" {
		t.Errorf("got since %q", cat.Since)
	}
	if cat.Pin["anthropic/claude-3-5-haiku"] != "lightweight" {
		t.Errorf("got pins %v", cat.Pin)
	}
	if want := filepath.Join(dirs.Cache, modelsFile); path != want {
		t.Errorf("got path %q, want %q", path, want)
	}
}

func TestModelsSyncCreatesTheCacheFileFromConfig(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	d, _, stderr, dirs := hermeticEnv(t, "[models.catalog]\nproviders = [\"anthropic\"]\n")
	r, err := d.loadConfig(&configFlags{})
	if err != nil {
		t.Fatal(err)
	}
	if code := d.syncModels(t.Context(), modelsOpts{cfg: r}, srv.endpoints(), fixedNow); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dirs.Cache, modelsFile)); err != nil {
		t.Errorf("no models.json in the cache dir: %v", err)
	}
}

func TestModelsSyncFetchesEverySourceAndWritesJSON(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	srv := newFakeServer(t)
	_, file, doc := repoDir(t, "models.json")
	d, stdout, stderr := testDeps()

	code := d.syncModels(ctx, plainOpts(t, file), srv.endpoints(), fixedNow)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if got := srv.count("/api.json"); got != 1 {
		t.Errorf("models.dev requests = %d, want 1", got)
	}
	if got := srv.count("/epoch.zip"); got != 1 {
		t.Errorf("epoch requests = %d, want 1", got)
	}
	// Needs catalog.ParseArenaPage returning the total (unit 1): the text
	// board has 150 rows, hence a second page.
	want := []string{"agent 0", "text_style_control 0", "text_style_control 100", "webdev 0"}
	if got := srv.arenaRequests(t); !slices.Equal(got, want) {
		t.Errorf("arena requests = %v, want %v", got, want)
	}

	if _, err := os.Stat(doc); !os.IsNotExist(err) {
		t.Errorf("doc created: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(mustRead(t, file), &parsed); err != nil {
		t.Errorf("models.json is not JSON: %v", err)
	}
	if !strings.Contains(stdout.String(), "✓ synced") {
		t.Errorf("stdout: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "synced 3 models · arena text_style_control 2026-11-05") {
		t.Errorf("stdout lacks the summary: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "untiered: anthropic/claude-sonnet-5-5 (Claude Sonnet 5.5): no ranking data") {
		t.Errorf("stdout lacks the untiered warning: %q", stdout.String())
	}
	if !strings.Contains(string(mustRead(t, file)), `"syncedAt": "2026-11-06"`) {
		t.Errorf("models.json lacks the sync date")
	}
}

func TestModelsSyncRebuildsExternalAndTiers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	srv := newFakeServer(t)
	_, file, _ := repoDir(t, "models.json")
	d, _, stderr := testDeps()

	code := d.syncModels(ctx, plainOpts(t, file), srv.endpoints(), fixedNow)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	data := string(mustRead(t, file))
	if !strings.Contains(data, `"eci": 155.5`) {
		t.Errorf("models.json lacks the Epoch score")
	}
	if !strings.Contains(data, `"pin"`) {
		t.Errorf("models.json lost the pin")
	}
	c, err := catalog.Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.TierOf("anthropic/claude-opus-5-5"); got != "powerful" {
		t.Errorf("opus tier = %q, want powerful", got)
	}
}

func TestModelsSyncPersistent500Exits1AndWritesNothing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	for _, path := range []string{"/api.json", "/rows", "/epoch.zip"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			srv := newFakeServer(t)
			srv.failFirst(path, 1000)
			_, file, doc := repoDir(t, "models.json")
			before := mustRead(t, file)
			d, _, stderr := testDeps()

			code := d.syncModels(ctx, plainOpts(t, file), srv.endpoints(), fixedNow)
			if code != 1 {
				t.Fatalf("exit %d, want 1", code)
			}
			if !strings.HasPrefix(stderr.String(), "error:") || !strings.Contains(stderr.String(), "500") || !strings.Contains(stderr.String(), path) {
				t.Errorf("stderr: %q", stderr.String())
			}
			if !bytes.Equal(mustRead(t, file), before) {
				t.Errorf("models.json changed")
			}
			if _, err := os.Stat(doc); err == nil {
				t.Errorf("doc written")
			}
		})
	}
}

func TestModelsSyncGivesUpAfterThreeAttempts(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	srv.failFirst("/api.json", 1000)
	_, file, _ := repoDir(t, "models.json")
	d, _, _ := testDeps()

	if code := d.syncModels(t.Context(), plainOpts(t, file), srv.endpoints(), fixedNow); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if got := srv.count("/api.json"); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestModelsSyncTwoFailuresThenSuccessRetriesAndSucceeds(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	srv := newFakeServer(t)
	srv.failFirst("/api.json", 2)
	_, file, doc := repoDir(t, "models.json")
	d, _, stderr := testDeps()

	code := d.syncModels(ctx, plainOpts(t, file), srv.endpoints(), fixedNow)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if got := srv.count("/api.json"); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
	if _, err := os.Stat(doc); !os.IsNotExist(err) {
		t.Errorf("doc created: %v", err)
	}
}

func TestModelsSyncCancelledContextExits1AndWritesNothing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	srv := newFakeServer(t)
	_, file, doc := repoDir(t, "models.json")
	before := mustRead(t, file)
	d, _, _ := testDeps()
	cctx, cancel := context.WithCancel(ctx)
	cancel()

	if code := d.syncModels(cctx, plainOpts(t, file), srv.endpoints(), fixedNow); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !bytes.Equal(mustRead(t, file), before) {
		t.Errorf("models.json changed")
	}
	if _, err := os.Stat(doc); err == nil {
		t.Errorf("doc written")
	}
}

func TestModelsSyncTierWithNoDataExits1AndWritesNothing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	srv := newFakeServer(t)
	_, file, doc := repoDir(t, "models-invalid.json")
	before := mustRead(t, file)
	d, _, stderr := testDeps()
	if code := d.syncModels(ctx, plainOpts(t, file), srv.endpoints(), fixedNow); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "anthropic/does-not-exist") {
		t.Errorf("stderr %q does not name the missing tier id", stderr.String())
	}
	if !bytes.Equal(before, mustRead(t, file)) {
		t.Error("models.json changed on a failed sync")
	}
	if _, err := os.Stat(doc); err == nil {
		t.Errorf("%s written on failure", doc)
	}
}

func TestModelsSyncFillsASeedCatalog(t *testing.T) { // regression: sync must not validate before it fetches
	t.Parallel()
	ctx := t.Context()
	srv := newFakeServer(t)
	_, file, _ := repoDir(t, "models-invalid.json")
	seed := strings.ReplaceAll(string(mustRead(t, file)), "anthropic/does-not-exist", "anthropic/claude-opus-5-5")
	if err := os.WriteFile(file, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	d, _, stderr := testDeps()
	if code := d.syncModels(ctx, plainOpts(t, file), srv.endpoints(), fixedNow); code != 0 {
		t.Fatalf("exit %d, want 0; stderr %q", code, stderr.String())
	}
	if !strings.Contains(string(mustRead(t, file)), `"name": "Claude Opus 5.5"`) {
		t.Error("models.json has no data for the tiered model")
	}
}

func TestArenaURL(t *testing.T) {
	t.Parallel()
	got := arenaURL("https://example.test/rows", "webdev", 200)
	want := "https://example.test/rows?config=webdev&dataset=lmarena-ai%2Fleaderboard-dataset&length=100&offset=200&split=latest"
	if got != want {
		t.Errorf("arenaURL = %s, want %s", got, want)
	}
}

func parseQuery(requestURI string) (url.Values, error) {
	_, q, _ := strings.Cut(requestURI, "?")
	return url.ParseQuery(q)
}

func TestRetryAfter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"5", 5 * time.Second},
		{" 7 ", 7 * time.Second},
		{"3600", maxRetryAfter},
		{"-1", 0},
		{"Wed, 21 Oct 2015 07:28:00 GMT", 0},
	}
	for _, tt := range tests {
		if got := retryAfter(tt.in); got != tt.want {
			t.Errorf("retryAfter(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestModelsSyncOtherSchemaExits1WithoutFetching(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t)
	_, file, _ := repoDir(t, "models.json")
	other := strings.Replace(string(mustRead(t, file)), `"schema": 2`, `"schema": 3`, 1)
	if err := os.WriteFile(file, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	d, _, _ := testDeps()
	if code := d.syncModels(t.Context(), plainOpts(t, file), srv.endpoints(), fixedNow); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if n := len(srv.log()); n != 0 {
		t.Errorf("%d requests for a catalog of another schema", n)
	}
}
