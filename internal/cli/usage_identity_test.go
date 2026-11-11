package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/provider"
	usg "github.com/giocaizzi/agents/internal/usage"
)

// rowStore is a fakeStore whose opencode database holds rows.
type rowStore struct {
	fakeStore

	rows []provider.OpencodeRow
}

func (s rowStore) OpencodeCredentials(string) ([]provider.OpencodeRow, error) { return s.rows, nil }

const (
	tokA = "tok-AAAA-fake-0001"
	tokB = "tok-BBBB-fake-0002"
)

func bearerToken(r *http.Request) string {
	return strings.TrimPrefix(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), "token ")
}

// usageServer answers the Anthropic and Copilot endpoints with limits that
// depend on the token: tokA is 31% used, tokB 62%.
func usageServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		used, left := 31, 69
		if bearerToken(r) == tokB {
			used, left = 62, 38
		}
		if strings.Contains(r.URL.Path, "copilot_internal") {
			fmt.Fprintf(w, `{"copilot_plan":"individual","quota_reset_date":"2026-12-01","quota_snapshots":{"premium_interactions":{"entitlement":100,"remaining":%d,"percent_remaining":%d}}}`, left, left)
			return
		}
		fmt.Fprintf(w, `{"five_hour":{"utilization":%d,"resets_at":"2026-11-06T14:14:00Z"}}`, used)
	}))
}

func failServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
}

// usedByHarness maps each row's harnesses to its first limit's used percentage.
func usedByHarness(t *testing.T, snaps []usg.Snapshot, wantStatus usg.Status) map[string]float64 {
	t.Helper()
	got := map[string]float64{}
	for _, s := range snaps {
		if len(s.Limits) == 0 || s.Limits[0].UsedPct == nil {
			t.Fatalf("row %v has no limit: %+v", s.Harnesses, s)
		}
		if s.Status != wantStatus {
			t.Errorf("row %v status = %s, want %s", s.Harnesses, s.Status, wantStatus)
		}
		got[strings.Join(s.Harnesses, ",")] = *s.Limits[0].UsedPct
	}
	return got
}

func TestUnlabelledAccountsOfOneProviderStayApart(t *testing.T) {
	t.Parallel()
	future := usageNow.Add(time.Hour).UnixMilli()
	claudeCreds := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d,"subscriptionType":"max"}}`, tokA, future)
	opencodeAnthropic := fmt.Sprintf(`{"type":"oauth","access":%q,"expires":%d}`, tokB, future)
	piCopilot := fmt.Sprintf(`{"github-copilot":{"type":"oauth","refresh":%q,"expires":%d}}`, tokA, future)
	opencodeCopilot := fmt.Sprintf(`{"type":"oauth","refresh":%q,"expires":%d}`, tokB, future)

	tests := []struct {
		name  string
		store rowStore
		want  func(usedA, usedB float64) map[string]float64
	}{
		{
			name: "claude code and an opencode anthropic login",
			store: rowStore{
				fakeStore: fakeStore{home: "/h", files: map[string]string{"/h/.claude/.credentials.json": claudeCreds}},
				rows:      []provider.OpencodeRow{{ID: "row1", IntegrationID: provider.Anthropic, Active: true, Value: []byte(opencodeAnthropic)}},
			},
			want: func(a, b float64) map[string]float64 { return map[string]float64{"claude": a, "opencode": b} },
		},
		{
			name: "pi and an opencode copilot login",
			store: rowStore{
				fakeStore: fakeStore{home: "/h", files: map[string]string{"/h/.pi/agent/auth.json": piCopilot}},
				rows:      []provider.OpencodeRow{{ID: "row2", IntegrationID: provider.GitHubCopilot, Active: true, Value: []byte(opencodeCopilot)}},
			},
			want: func(a, b float64) map[string]float64 { return map[string]float64{"pi": a, "opencode": b} },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertDistinctIDs(t, provider.Discover(tc.store))
			// Copilot percent_remaining 69 and 38 are 31% and 62% used.
			assertIdentityFlow(t, tc.store, tc.want(31, 62))
		})
	}
}

// assertDistinctIDs checks Discover found two unlabelled accounts with
// distinct, token-free ids.
func assertDistinctIDs(t *testing.T, accounts []provider.Account) {
	t.Helper()
	if len(accounts) != 2 || accounts[0].Label != "" || accounts[1].Label != "" {
		t.Fatalf("Discover = %+v, want two unlabelled accounts", accounts)
	}
	ids := map[string]bool{}
	for _, a := range accounts {
		if a.ID == "" || strings.Contains(a.ID, tokA) || strings.Contains(a.ID, tokB) {
			t.Errorf("bad account id %q", a.ID)
		}
		ids[a.ID] = true
	}
	if len(ids) != 2 {
		t.Fatalf("account ids collide: %+v", ids)
	}
}

// assertIdentityFlow runs usage on a healthy server, then
// on a failing server, and checks every step has two rows with their own limits.
func assertIdentityFlow(t *testing.T, store provider.Store, want map[string]float64) {
	t.Helper()
	ok, bad := usageServer(), failServer()
	defer ok.Close()
	defer bad.Close()
	d, out, errb, path := usageDeps(t)

	if code := d.runUsageWith(t.Context(), nil, store, testFetcher(t, ok)); code != 0 {
		t.Fatalf("live exit = %d, stderr %q", code, errb)
	}
	cached, err := readUsageCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, usedByHarness(t, cached.Snapshots, usg.StatusOK)); diff != "" {
		t.Errorf("live rows (-want +got):\n%s", diff)
	}

	out.Reset()
	if code := d.runUsageWith(t.Context(), nil, store, testFetcher(t, bad)); code != 0 {
		t.Fatalf("failing live exit = %d, stderr %q", code, errb)
	}
	stale, err := readUsageCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, usedByHarness(t, stale.Snapshots, usg.StatusStale)); diff != "" {
		t.Errorf("stale rows (-want +got):\n%s", diff)
	}
	for _, s := range stale.Snapshots {
		if s.ID == "" || strings.Contains(s.ID, tokA) || strings.Contains(s.ID, tokB) {
			t.Errorf("snapshot id %q", s.ID)
		}
	}
	if strings.Contains(out.String(), tokA) || strings.Contains(out.String(), tokB) {
		t.Error("a token reached the output")
	}
}
