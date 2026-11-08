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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/giocaizzi/agents/internal/catalog"
	"github.com/giocaizzi/agents/internal/term"
)

const (
	arenaDataset  = "lmarena-ai/leaderboard-dataset"
	arenaPageSize = 100
	fetchAttempts = 3
	fetchLimit    = 64 << 20 // bytes read from one response
	fetchInFlight = 4
	helpCmd       = "help"
)

// endpoints are the data sources of models sync; tests point them at httptest.
type endpoints struct {
	modelsDev string
	arenaRows string
	epoch     string
	backoff   []time.Duration // waits before attempt 2 and 3
}

func defaultEndpoints() endpoints {
	return endpoints{
		modelsDev: "https://models.dev/api.json",
		arenaRows: "https://datasets-server.huggingface.co/rows",
		epoch:     "https://epoch.ai/data/benchmark_data.zip",
		backoff:   []time.Duration{2 * time.Second, 8 * time.Second},
	}
}

type modelsOpts struct {
	file, doc string
}

func (d Deps) runModels(ctx context.Context, args []string) int {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "sync":
			return d.runModelsSync(ctx, args[1:])
		case "render":
			return d.runModelsRender(args[1:])
		case helpCmd:
			st := term.NewStyle(d.Stdout, d.Getenv)
			fmt.Fprint(d.Stdout, usage(st))
			return 0
		default:
			st := term.NewStyle(d.Stderr, d.Getenv)
			fmt.Fprintf(d.Stderr, "%sunknown models subcommand: %s%s\n\n", st.R, args[0], st.O)
			fmt.Fprint(d.Stderr, usage(st))
			return 2
		}
	}
	return d.runModelsShow(args)
}

func (d Deps) modelsFail(format string, a ...any) int {
	st := term.NewStyle(d.Stderr, d.Getenv)
	fmt.Fprintf(d.Stderr, "%serror:%s %s\n", st.R, st.O, fmt.Sprintf(format, a...))
	return 1
}

func (d Deps) flagFail(err error) int {
	fmt.Fprintf(d.Stderr, "flag parse error: %v\n", err)
	return 2
}

// loadCatalog reads models.json and, when validate is set, validates it. Sync
// skips validation: it is what fills models and tiers.
func (d Deps) loadCatalog(explicit string, validate bool) (catalog.Catalog, string, error) {
	path, err := d.findModelsJSON(explicit)
	if err != nil {
		return catalog.Catalog{}, "", err
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return catalog.Catalog{}, "", fmt.Errorf("read %s: %w", path, err)
	}
	c, err := catalog.Parse(data)
	if err != nil {
		return catalog.Catalog{}, "", fmt.Errorf("parse %s: %w", path, err)
	}
	if !validate {
		return c, path, nil
	}
	if err := c.Validate(); err != nil {
		return catalog.Catalog{}, "", fmt.Errorf("%s:\n%w", path, err)
	}
	return c, path, nil
}

func (d Deps) findModelsJSON(explicit string) (string, error) {
	if explicit != "" {
		cleaned := filepath.Clean(explicit)
		if _, err := os.Stat(cleaned); err != nil {
			return "", fmt.Errorf("stat %s: %w", explicit, err)
		}
		return cleaned, nil
	}

	var candidates []string
	if src := d.Getenv("CHEZMOI_SOURCE_DIR"); src != "" {
		candidates = append(candidates, filepath.Join(src, ".chezmoidata", "models.json"))
	}
	candidates = append(candidates,
		filepath.Join("home", ".chezmoidata", "models.json"),
		filepath.Join(".chezmoidata", "models.json"),
		filepath.Join("..", "home", ".chezmoidata", "models.json"),
		filepath.Join("..", "..", "home", ".chezmoidata", "models.json"),
	)
	for _, cand := range candidates {
		cleaned := filepath.Clean(cand)
		if _, err := os.Stat(cleaned); err == nil {
			return cleaned, nil
		}
	}
	return "", errors.New("models.json not found")
}

// docPath is docs/MODELS.md of the repo whose home/.chezmoidata holds file,
// unless explicit.
func docPath(file, explicit string) string {
	if explicit != "" {
		return filepath.Clean(explicit)
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "docs", "MODELS.md")
}

func (d Deps) runModelsShow(args []string) int {
	fs := newFlags("models")
	file := fs.String("file", "", "path to models.json")
	if err := fs.Parse(args); err != nil {
		return d.flagFail(err)
	}
	c, _, err := d.loadCatalog(*file, true)
	if err != nil {
		return d.modelsFail("%v", err)
	}
	catalog.WriteTerminal(d.Stdout, term.NewStyle(d.Stdout, d.Getenv), c)
	return 0
}

func (d Deps) runModelsRender(args []string) int {
	fs := newFlags("models render")
	check := fs.Bool("check", false, "compare the doc instead of writing it")
	file := fs.String("file", "", "path to models.json")
	doc := fs.String("doc", "", "path to docs/MODELS.md")
	if err := fs.Parse(args); err != nil {
		return d.flagFail(err)
	}
	c, path, err := d.loadCatalog(*file, true)
	if err != nil {
		return d.modelsFail("%v", err)
	}
	docFile := docPath(path, *doc)
	want := catalog.Markdown(c)
	st := term.NewStyle(d.Stdout, d.Getenv)

	if *check {
		got, err := os.ReadFile(filepath.Clean(docFile))
		if err != nil || !bytes.Equal(got, want) {
			return d.modelsFail("%s is out of date: run agents models render", docFile)
		}
		fmt.Fprintf(d.Stdout, "%s✓%s %s is up to date\n", st.G, st.O, docFile)
		return 0
	}
	if err := writeFilesAtomic([]fileWrite{{docFile, want}}); err != nil {
		return d.modelsFail("%v", err)
	}
	fmt.Fprintf(d.Stdout, "%s✓%s wrote %s\n", st.G, st.O, docFile)
	return 0
}

func (d Deps) runModelsSync(ctx context.Context, args []string) int {
	fs := newFlags("models sync")
	file := fs.String("file", "", "path to models.json")
	doc := fs.String("doc", "", "path to docs/MODELS.md")
	if err := fs.Parse(args); err != nil {
		return d.flagFail(err)
	}
	return d.syncModels(ctx, modelsOpts{file: *file, doc: *doc}, defaultEndpoints(), time.Now)
}

// syncModels fetches every source, then writes models.json and the doc; any
// failure before the writes leaves both files untouched.
func (d Deps) syncModels(ctx context.Context, opts modelsOpts, ep endpoints, now func() time.Time) int {
	c, path, err := d.loadCatalog(opts.file, false)
	if err != nil {
		return d.modelsFail("%v", err)
	}
	if c.Schema != catalog.Schema {
		return d.modelsFail("%s: schema %d, want %d", path, c.Schema, catalog.Schema)
	}
	docFile := docPath(path, opts.doc)

	f := fetcher{client: &http.Client{Timeout: 60 * time.Second}, backoff: ep.backoff}
	src := catalog.Sources{Today: now().Format("2006-01-02")}

	if src.ModelsDev, err = f.modelsDev(ctx, ep, c.Providers); err != nil {
		return d.modelsFail("%v", err)
	}
	if src.Arena, err = f.arena(ctx, ep); err != nil {
		return d.modelsFail("%v", err)
	}
	if src.Epoch, err = f.epoch(ctx, ep); err != nil {
		return d.modelsFail("%v", err)
	}

	next, rep := catalog.Apply(c, src)
	if len(rep.Missing) > 0 {
		return d.modelsFail("pinned models with no data: %s (fix the pin id in models.json)", strings.Join(rep.Missing, ", "))
	}
	if err = next.Validate(); err != nil {
		return d.modelsFail("%v", err)
	}
	jsonData, err := catalog.Marshal(next)
	if err != nil {
		return d.modelsFail("%v", err)
	}
	if err = writeFilesAtomic([]fileWrite{{path, jsonData}, {docFile, catalog.Markdown(next)}}); err != nil {
		return d.modelsFail("%v", err)
	}

	st := term.NewStyle(d.Stdout, d.Getenv)
	fmt.Fprintf(d.Stdout, "%s✓%s synced %d models · arena %s %s\n",
		st.G, st.O, len(next.Models), next.Sources.Arena.Board, next.Sources.Arena.PublishDate)
	for _, id := range rep.Untiered {
		st.Warn(d.Stdout, fmt.Sprintf("untiered: %s (%s): %s", id, next.Models[id].Name, catalog.UntieredReason(next.Models[id])))
	}
	for _, id := range rep.Stale {
		st.Warn(d.Stdout, fmt.Sprintf("stale: %s (no provider lists it; kept)", id))
	}
	if len(rep.Unmatched) > 0 {
		st.Warn(d.Stdout, fmt.Sprintf("unmatched source names: %s%s%s", st.D, strings.Join(rep.Unmatched, ", "), st.O))
	}
	return 0
}

type fileWrite struct {
	path string
	data []byte
}

// writeFilesAtomic stages every file as a temp file next to its target, then
// renames them all, so a failure while staging leaves every target untouched.
// A failed later rename can leave the doc behind models.json: `models render
// -check` in make lint catches that, and `models render` repairs it.
func writeFilesAtomic(files []fileWrite) error {
	staged := make([]string, 0, len(files))
	defer func() {
		for _, tmp := range staged {
			_ = os.Remove(tmp) // already gone after a successful rename
		}
	}()
	for _, f := range files {
		dir := filepath.Dir(f.path)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		tmp, err := os.CreateTemp(dir, "."+filepath.Base(f.path)+".*")
		if err != nil {
			return fmt.Errorf("stage %s: %w", f.path, err)
		}
		staged = append(staged, tmp.Name())
		// Keep the target's mode; a new file gets the repo's usual 0644, not CreateTemp's 0600.
		mode := os.FileMode(0o644)
		if fi, err := os.Stat(f.path); err == nil {
			mode = fi.Mode().Perm()
		}
		merr := tmp.Chmod(mode)
		_, werr := tmp.Write(f.data)
		cerr := tmp.Close()
		if err := errors.Join(merr, werr, cerr); err != nil {
			return fmt.Errorf("stage %s: %w", f.path, err)
		}
	}
	for i, f := range files {
		if err := os.Rename(staged[i], f.path); err != nil {
			return fmt.Errorf("write %s: %w", f.path, err)
		}
	}
	return nil
}

// fetcher does the HTTP work of a sync.
type fetcher struct {
	client  *http.Client
	backoff []time.Duration
}

func (f fetcher) modelsDev(ctx context.Context, ep endpoints, providers []string) ([]catalog.ProviderModel, error) {
	body, err := f.get(ctx, ep.modelsDev)
	if err != nil {
		return nil, err
	}
	rows, err := catalog.ParseModelsDev(body, providers)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", ep.modelsDev, err)
	}
	return rows, nil
}

func (f fetcher) epoch(ctx context.Context, ep endpoints) ([]catalog.EpochRow, error) {
	body, err := f.get(ctx, ep.epoch)
	if err != nil {
		return nil, err
	}
	rows, err := catalog.ParseEpoch(body)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", ep.epoch, err)
	}
	return rows, nil
}

func arenaURL(base, board string, offset int) string {
	q := url.Values{}
	q.Set("dataset", arenaDataset)
	q.Set("config", board)
	q.Set("split", "latest")
	q.Set("offset", strconv.Itoa(offset))
	q.Set("length", strconv.Itoa(arenaPageSize))
	return base + "?" + q.Encode()
}

// arena fetches every page of the text, webdev and agent boards, at most
// fetchInFlight requests at a time, and returns rows in board then offset order.
func (f fetcher) arena(ctx context.Context, ep endpoints) ([]catalog.ArenaRow, error) {
	boards := []string{catalog.ArenaBoard, "webdev", "agent"}
	pages := make([][][]catalog.ArenaRow, len(boards))
	totals := make([]int, len(boards))

	fetchPage := func(ctx context.Context, b, page int) error {
		body, err := f.get(ctx, arenaURL(ep.arenaRows, boards[b], page*arenaPageSize))
		if err != nil {
			return err
		}
		rows, total, err := catalog.ParseArenaPage(boards[b], body)
		if err != nil {
			return fmt.Errorf("parse arena %s page %d: %w", boards[b], page, err)
		}
		pages[b][page] = rows
		if page == 0 {
			totals[b] = total
		}
		return nil
	}

	// Page 0 of every board first: it carries the row total.
	for b := range boards {
		pages[b] = make([][]catalog.ArenaRow, 1)
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(fetchInFlight)
	for b := range boards {
		g.Go(func() error { return fetchPage(gctx, b, 0) })
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	g, gctx = errgroup.WithContext(ctx)
	g.SetLimit(fetchInFlight)
	for b := range boards {
		n := (totals[b] + arenaPageSize - 1) / arenaPageSize
		if n > 1 {
			pages[b] = append(pages[b], make([][]catalog.ArenaRow, n-1)...)
		}
		for page := 1; page < n; page++ {
			g.Go(func() error { return fetchPage(gctx, b, page) })
		}
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	var out []catalog.ArenaRow
	for _, board := range pages {
		for _, rows := range board {
			out = append(out, rows...)
		}
	}
	return out, nil
}

// get fetches rawURL with up to fetchAttempts tries, waiting f.backoff between them.
func (f fetcher) get(ctx context.Context, rawURL string) ([]byte, error) {
	var err error
	for attempt := range fetchAttempts {
		if attempt > 0 {
			var wait time.Duration
			if attempt-1 < len(f.backoff) {
				wait = f.backoff[attempt-1]
			}
			if se, ok := errors.AsType[*statusError](err); ok {
				wait = max(wait, se.retryAfter)
			}
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return nil, fmt.Errorf("get %s: %w", rawURL, ctx.Err())
			case <-t.C:
			}
		}
		var body []byte
		if body, err = f.getOnce(ctx, rawURL); err == nil {
			return body, nil
		}
		if ctx.Err() != nil {
			return nil, err
		}
	}
	return nil, err
}

func (f fetcher) getOnce(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", "agents")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &statusError{url: rawURL, status: resp.Status, retryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchLimit))
	if err != nil {
		return nil, fmt.Errorf("get %s: read body: %w", rawURL, err)
	}
	return body, nil
}

// statusError is a non-2xx answer; retryAfter is the server's Retry-After, if any.
type statusError struct {
	url, status string
	retryAfter  time.Duration
}

func (e *statusError) Error() string { return "get " + e.url + ": " + e.status }

// maxRetryAfter caps how long a Retry-After header can make a sync wait.
const maxRetryAfter = 30 * time.Second

// retryAfter reads a Retry-After header in seconds (the HTTP-date form is
// ignored), capped at maxRetryAfter.
func retryAfter(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 0
	}
	return min(time.Duration(n)*time.Second, maxRetryAfter)
}
