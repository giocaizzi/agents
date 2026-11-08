package cli

import (
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	_ "modernc.org/sqlite"

	"github.com/giocaizzi/agents/internal/provider"
)

func testStore(t *testing.T) osStore {
	t.Helper()
	return osStore{
		getenv: func(k string) string { return map[string]string{"HOME_TEST": "yes"}[k] },
		goos:   "linux",
		home:   t.TempDir(),
		now:    func() time.Time { return time.Unix(1700000000, 0) },
	}
}

func TestOSStoreFiles(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	path := filepath.Join(s.Home(), "a.txt")
	if s.Exists(path) {
		t.Fatal("Exists true for a missing file")
	}
	if _, err := s.ReadFile(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadFile missing: err = %v, want fs.ErrNotExist", err)
	}
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := s.ReadFile(path)
	if err != nil || string(b) != "hello" || !s.Exists(path) {
		t.Fatalf("ReadFile = %q, %v", b, err)
	}
	if s.GOOS() != "linux" || s.Getenv("HOME_TEST") != "yes" || s.Now().Unix() != 1700000000 {
		t.Error("Deps passthroughs are wrong")
	}
}

func TestOSStoreSecretEmptyAccountNeedsDarwin(t *testing.T) {
	t.Parallel()
	_, err := testStore(t).Secret("svc", "")
	if err == nil {
		t.Fatal("expected an error for an empty account off macOS")
	}
}

func TestDecodeKeyringValue(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"plain":                                 "plain",
		"go-keyring-base64:" + b64("{\"a\":1}"): "{\"a\":1}",
		"go-keyring-encoded:" + hex.EncodeToString([]byte("sek")): "sek",
		"go-keyring-base64:!!notbase64":                           "go-keyring-base64:!!notbase64",
	}
	for in, want := range tests {
		if got := decodeKeyringValue(in); got != want {
			t.Errorf("decodeKeyringValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// writeCredentialDB creates opencode's credential table at path with two rows.
func writeCredentialDB(t *testing.T, path string) {
	t.Helper()
	// A plain file path is not safe in a DSN when it holds ?, # or %.
	dsn := (&url.URL{Scheme: "file", Path: sqlitePath(path)}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	for _, q := range []string{
		`CREATE TABLE credential (id TEXT, integration_id TEXT, label TEXT, active INTEGER, value TEXT)`,
		`INSERT INTO credential VALUES ('c1', 'openai', 'me@example.com', 1, '{"type":"oauth","access":"fake"}')`,
		`INSERT INTO credential VALUES ('c2', 'mcp_foo', NULL, 0, '{"type":"key"}')`,
	} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
}

var wantCredentialRows = []provider.OpencodeRow{
	{ID: "c1", IntegrationID: "openai", Label: "me@example.com", Active: true, Value: []byte(`{"type":"oauth","access":"fake"}`)},
	{ID: "c2", IntegrationID: "mcp_foo", Active: false, Value: []byte(`{"type":"key"}`)},
}

func TestOpencodeCredentials(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	path := filepath.Join(s.Home(), "opencode.db")
	writeCredentialDB(t, path)

	got, err := s.OpencodeCredentials(path)
	if err != nil {
		t.Fatalf("OpencodeCredentials: %v", err)
	}
	if diff := cmp.Diff(wantCredentialRows, got); diff != "" {
		t.Errorf("rows (-want +got):\n%s", diff)
	}
}

func TestOpencodeCredentialsAwkwardPath(t *testing.T) {
	t.Parallel()
	name := "odd#dir%41"
	if runtime.GOOS != "windows" {
		name += "?x"
	}
	dir := filepath.Join(t.TempDir(), name)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "opencode.db")
	writeCredentialDB(t, path)

	got, err := testStore(t).OpencodeCredentials(path)
	if err != nil {
		t.Fatalf("OpencodeCredentials: %v", err)
	}
	if diff := cmp.Diff(wantCredentialRows, got); diff != "" {
		t.Errorf("rows (-want +got):\n%s", diff)
	}
}

func TestSqliteDSN(t *testing.T) {
	t.Parallel()
	const q = "?_pragma=busy_timeout%282000%29&mode=ro"
	tests := []struct{ path, want string }{
		{"/home/me/opencode.db", "file:///home/me/opencode.db" + q},
		{"/a?b#c%d/x.db", "file:///a%3Fb%23c%25d/x.db" + q},
		{"C:/Users/me/x.db", "file:///C:/Users/me/x.db" + q},
	}
	for _, tt := range tests {
		if got := sqliteDSN(tt.path); got != tt.want {
			t.Errorf("sqliteDSN(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestOpencodeCredentialsMissingDB(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	_, err := s.OpencodeCredentials(filepath.Join(s.Home(), "nope.db"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

// stubSecurity returns a script that mimics the security tool. With -a it
// prints fresh-token for wantAcct and otherwise exits acctExit; without -a it
// prints stale-token (another account's item) when anyExit is 0, else exits
// anyExit. Every failure writes stderrMarker to stderr, which must never reach
// an error.
func stubSecurity(t *testing.T, wantAcct string, acctExit, anyExit int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub")
	}
	script := fmt.Sprintf(`#!/bin/sh
acct=""
while [ $# -gt 0 ]; do
  if [ "$1" = "-a" ]; then acct="$2"; fi
  shift
done
if [ -z "$acct" ]; then
  if [ %[3]d -eq 0 ]; then echo stale-token; exit 0; fi
  echo %[4]s >&2; exit %[3]d
fi
if [ "$acct" = "%[1]s" ]; then echo fresh-token; exit 0; fi
echo %[4]s >&2; exit %[2]d
`, wantAcct, acctExit, anyExit, stderrMarker)
	path := filepath.Join(t.TempDir(), "security")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

const stderrMarker = "stub-stderr-text"

// Deliberately serial: a fork racing the still-open write descriptor of a
// freshly written stub makes its exec fail with ETXTBSY, which Secret rightly
// reports as "not readable". Serial top-level tests run while the parallel
// ones are paused, so no other fork can overlap the stub writes.
func TestSecretDarwinAnyAccount(t *testing.T) { //nolint:paralleltest // see above
	tests := []struct {
		name     string
		user     string
		wantAcct string
		acctExit int
		anyExit  int
		want     string
		wantErr  string
	}{
		{name: "current user's item first", user: "alice", wantAcct: "alice", acctExit: 44, want: "fresh-token"},
		{name: "item not found falls back to any account", user: "alice", wantAcct: "bob", acctExit: 44, want: "stale-token"},
		{name: "denied prompt does not fall back", user: "alice", wantAcct: "bob", acctExit: 128, wantErr: "not readable"},
		{name: "auth failure does not fall back", user: "alice", wantAcct: "bob", acctExit: 51, wantErr: "not readable"},
		// The -a lookup would fail with 128: an empty user must skip it.
		{name: "empty user skips the account lookup", user: "", wantAcct: "bob", acctExit: 128, want: "stale-token"},
		{name: "both lookups fail", user: "alice", wantAcct: "bob", acctExit: 44, anyExit: 44, wantErr: "not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testStore(t)
			s.goos = "darwin"
			s.timeout = time.Minute // loaded CI hosts spawn the stub slowly
			s.user = func() string { return tt.user }
			s.securityBin = stubSecurity(t, tt.wantAcct, tt.acctExit, tt.anyExit)
			got, err := s.Secret("svc", "")
			if got != tt.want {
				t.Errorf("Secret = %q, want %q", got, tt.want)
			}
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Secret error = %v, want none", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("Secret error = %v, want %q", err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), stderrMarker) {
				t.Errorf("Secret error %q echoes stderr", err)
			}
		})
	}
}

func TestOSStoreListDir(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	for _, d := range []string{".claude-b", ".claude-a", ".other"} {
		if err := os.Mkdir(filepath.Join(s.Home(), d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{".claude-a", ".claude-b", ".other"}
	if diff := cmp.Diff(want, s.ListDir(s.Home())); diff != "" {
		t.Errorf("ListDir (-want +got):\n%s", diff)
	}
	if got := s.ListDir(filepath.Join(s.Home(), "missing")); got != nil {
		t.Errorf("ListDir(missing) = %v, want nil", got)
	}
}

func TestOSStoreCanonical(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "real")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks unsupported: %v", err)
		}
		t.Fatal(err)
	}
	if got := s.Canonical(link); got != target {
		t.Errorf("Canonical(link) = %q, want %q", got, target)
	}
	missing := filepath.Join(dir, "a", "..", "nope")
	if got, want := s.Canonical(missing), filepath.Join(dir, "nope"); got != want {
		t.Errorf("Canonical(missing) = %q, want %q", got, want)
	}
}
