package apply

import (
	"math/rand/v2"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// lcsLen is the textbook table, used only to check that lineDiff is minimal.
func lcsLen(a, b []string) int {
	t := make([][]int, len(a)+1)
	for i := range t {
		t[i] = make([]int, len(b)+1)
	}
	for i := range slices.Backward(a) {
		for j := range slices.Backward(b) {
			if a[i] == b[j] {
				t[i][j] = t[i+1][j+1] + 1
			} else {
				t[i][j] = max(t[i+1][j], t[i][j+1])
			}
		}
	}
	return t[0][0]
}

func randomLines(rng *rand.Rand) []string {
	out := make([]string, rng.IntN(14))
	for i := range out {
		out[i] = string(rune('a' + rng.IntN(4)))
	}
	return out
}

// replay splits a script back into the two sequences and counts the shared lines.
func replay(ops []diffOp) (from, to []string, common int) {
	for _, op := range ops {
		if op.kind != '+' {
			from = append(from, op.text)
		}
		if op.kind != '-' {
			to = append(to, op.text)
		}
		if op.kind == ' ' {
			common++
		}
	}
	return from, to, common
}

func TestLineDiffIsAMinimalScript(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(1, 2))
	for range 400 {
		a, b := randomLines(rng), randomLines(rng)
		from, to, common := replay(lineDiff(a, b))
		if !slices.Equal(from, a) && (len(from) > 0 || len(a) > 0) || !slices.Equal(to, b) && (len(to) > 0 || len(b) > 0) {
			t.Fatalf("script does not rebuild the inputs: a=%v b=%v", a, b)
		}
		if want := lcsLen(a, b); common != want {
			t.Fatalf("a=%v b=%v: %d common lines, want %d", a, b, common, want)
		}
	}
}

func TestDiffHidesSecretsAndUnmanagedValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	live := "{\n  \"primaryApiKey\": \"sk-live-unmanaged\",\n  \"projects\": {\"/p\": {\"n\": 1}},\n  \"mcpServers\": {}\n}\n"
	writeFileT(t, path, live)
	managed := map[string]any{"mcpServers": map[string]any{"docs": map[string]any{
		"headers": map[string]any{"Authorization": `Bearer s3"cret`},
	}}}
	tg := Target{Path: path, Format: JSON, Managed: managed, Sensitive: []string{`Bearer s3"cret`, "s3\"cret"}}
	e := engineIn(dir)
	e.DryRun = true
	res := runOne(t, e, tg)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	for _, leak := range []string{"sk-live-unmanaged", "Bearer", `s3\"`} {
		if strings.Contains(res.Diff, leak) {
			t.Errorf("diff leaks %q:\n%s", leak, res.Diff)
		}
	}
	if !strings.Contains(res.Diff, `"primaryApiKey": "«unmanaged»"`) || !strings.Contains(res.Diff, "«redacted»") {
		t.Errorf("diff = \n%s", res.Diff)
	}
}

func TestDiffRedactsOldValueOfRotatedSecret(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	writeFileT(t, path, "{\n  \"h\": {\"Authorization\": \"Bearer old-token-1234\"}\n}\n")
	e := engineIn(dir)
	e.DryRun = true
	res := runOne(t, e, Target{
		Path: path, Format: JSON,
		Managed:   map[string]any{"h": map[string]any{"Authorization": "Bearer new-token-5678"}},
		Sensitive: []string{"new-token-5678"},
	})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	for _, tok := range []string{"old-token-1234", "new-token-5678"} {
		if strings.Contains(res.Diff, tok) {
			t.Errorf("diff leaks %s:\n%s", tok, res.Diff)
		}
	}
	if !strings.Contains(res.Diff, redactedMark) {
		t.Errorf("diff = \n%s", res.Diff)
	}
}

func TestTOMLDiffMasksUnmanaged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFileT(t, path, "token = 'live-secret'\n\n[tools]\nx = 'a'\n")
	e := engineIn(dir)
	e.DryRun = true
	res := runOne(t, e, Target{Path: path, Format: TOML, Managed: map[string]any{"tools": map[string]any{"x": "b"}}})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if strings.Contains(res.Diff, "live-secret") || !strings.Contains(res.Diff, `token = "«unmanaged»"`) {
		t.Errorf("diff = \n%s", res.Diff)
	}
	if !strings.Contains(res.Diff, "-x = 'a'") || !strings.Contains(res.Diff, `+x = "b"`) {
		t.Errorf("managed change not shown:\n%s", res.Diff)
	}
}

func TestRedactEscapedForms(t *testing.T) {
	t.Parallel()
	secret := `a"b\c&d <e> f/g?h`
	toml := tomlString(secret)
	forms := map[string]string{
		"raw":      secret,
		"json":     jsonInner(secret, false),
		"jsonhtml": jsonInner(secret, true),
		"toml":     toml[1 : len(toml)-1],
		"query":    url.QueryEscape(secret),
		"path":     url.PathEscape(secret),
	}
	for name, form := range forms {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := redact("x = "+form+"\n", []string{secret})
			if strings.Contains(got, form) || !strings.Contains(got, redactedMark) {
				t.Errorf("redact left %s form: %q", name, got)
			}
		})
	}
}

func TestDiffHeadersNeverRedacted(t *testing.T) {
	t.Parallel()
	// The secrets also appear in the path and the hunk header.
	path := "/home/u1/f1.json"
	got := unifiedDiff(path, "a = 1\n", "a = 2\n", func(l string) string { return redact(l, []string{"1", "f1", "@@"}) })
	for _, want := range []string{"--- " + path + "\n", "+++ " + path + "\n", "@@ -1 +1 @@\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("header %q was altered:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "-a = "+redactedMark+"\n") {
		t.Errorf("content not redacted:\n%s", got)
	}
}
