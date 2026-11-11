package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	usg "github.com/giocaizzi/agents/internal/usage"
)

func claudeCredsFile(token, subscription string) string {
	return fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d,"subscriptionType":%q}}`,
		token, usageNow.Add(time.Hour).UnixMilli(), subscription)
}

func TestTwoClaudeProfilesEndToEnd(t *testing.T) {
	t.Parallel()
	store := fakeStore{home: "/h", files: map[string]string{
		"/h/.claude/.credentials.json":      claudeCredsFile(tokA, "max"),
		"/h/.claude-work/.credentials.json": claudeCredsFile(tokB, "max"),
	}}
	assertIdentityFlow(t, store, map[string]float64{"claude": 31, "claude:work": 62})
}

func TestTwoClaudeProfilesNeverLeakTokens(t *testing.T) {
	t.Parallel()
	store := fakeStore{home: "/h", files: map[string]string{
		"/h/.claude/.credentials.json":      claudeCredsFile(tokA, "max"),
		"/h/.claude-work/.credentials.json": claudeCredsFile(tokB, "enterprise"),
	}}
	srv := usageServer()
	defer srv.Close()
	d, out, errb, path := usageDeps(t)
	if code := d.runUsageWith(t.Context(), nil, store, testFetcher(t, srv)); code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, errb)
	}
	cache, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"stdout": out.String(), "stderr": errb.String(), "cache": string(cache)} {
		for _, tok := range []string{tokA, tokB} {
			if strings.Contains(text, tok) {
				t.Errorf("%s holds a token", name)
			}
		}
	}
	if !strings.Contains(out.String(), "claude:work") {
		t.Errorf("output lacks the profile name:\n%s", out)
	}
}

// An enterprise seat whose usage endpoint answers with no per-seat window gets
// no numbers, only the note.
func TestOnlyEnterpriseProfileShowsSeatWithoutWindows(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"five_hour":null,"seven_day":null,"seven_day_opus":null}`)
	}))
	defer srv.Close()
	store := fakeStore{home: "/h", files: map[string]string{
		"/h/.claude-corp/.credentials.json": claudeCredsFile(tokA, "enterprise"),
	}}
	d, out, errb, path := usageDeps(t)
	if code := d.runUsageWith(t.Context(), nil, store, testFetcher(t, srv)); code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, errb)
	}
	cached, err := readUsageCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached.Snapshots) != 1 {
		t.Fatalf("got %d rows, want 1", len(cached.Snapshots))
	}
	s := cached.Snapshots[0]
	if diff := cmp.Diff([]string{"claude"}, s.Harnesses); diff != "" {
		t.Errorf("harnesses (-want +got):\n%s", diff)
	}
	if s.Plan.Kind != usg.KindEnterprise || s.Plan.Name != "Enterprise seat" {
		t.Errorf("plan = %+v, want the enterprise seat", s.Plan)
	}
	if len(s.Limits) == 0 {
		t.Fatal("no limits")
	}
	for _, l := range s.Limits {
		if l.UsedPct != nil || !strings.Contains(l.Note, "no per-seat window") {
			t.Errorf("limit %+v: want a note and no numbers", l)
		}
	}
	if !strings.Contains(out.String(), "Enterprise seat") || !strings.Contains(out.String(), "no per-seat window") {
		t.Errorf("output:\n%s", out)
	}
}
