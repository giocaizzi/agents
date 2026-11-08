package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giocaizzi/agents/internal/provider"
)

func TestOSStoreReadFileLimits(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	write := func(name string, n int) string {
		p := filepath.Join(s.Home(), name)
		if err := os.WriteFile(p, bytes.Repeat([]byte("x"), n), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if b, err := s.ReadFile(write("ok.json", 100)); err != nil || len(b) != 100 {
		t.Errorf("normal file: len %d, err %v", len(b), err)
	}
	if b, err := s.ReadFile(write("mid.json", 2<<20)); err != nil || len(b) != 2<<20 {
		t.Errorf("2 MiB file: len %d, err %v", len(b), err)
	}
	if _, err := s.ReadFile(s.Home()); !errors.Is(err, errNotRegular) {
		t.Errorf("directory: err = %v, want errNotRegular", err)
	}
	if _, err := s.ReadFile(filepath.Join(s.Home(), "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: err = %v, want fs.ErrNotExist", err)
	}
}

// sparse makes a file of n bytes without writing them.
func sparse(t *testing.T, path string, n int64) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(n); err != nil {
		f.Close()
		t.Skipf("sparse files unsupported: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOSStoreReadFileCap(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	atCap := filepath.Join(s.Home(), "max.json")
	sparse(t, atCap, maxReadFile)
	if b, err := s.ReadFile(atCap); err != nil || len(b) != maxReadFile {
		t.Errorf("file of exactly the cap: len %d, err %v", len(b), err)
	}
	over := filepath.Join(s.Home(), "big.json")
	sparse(t, over, maxReadFile+1)
	if _, err := s.ReadFile(over); !errors.Is(err, provider.ErrTooLarge) {
		t.Errorf("oversized file: err = %v, want provider.ErrTooLarge", err)
	}
}

const claudeCredsJSON = `{"claudeAiOauth":{"accessToken":"tok-1","subscriptionType":"pro","expiresAt":4102444800000}}`

func writeClaudeFixture(t *testing.T, s osStore, profileJSON func(path string)) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(s.Home(), ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Home(), ".claude", ".credentials.json"), []byte(claudeCredsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	profileJSON(filepath.Join(s.Home(), ".claude.json"))
}

func claudeRow(t *testing.T, s osStore) provider.Account {
	t.Helper()
	for _, a := range provider.Discover(s) {
		if a.Provider == provider.Anthropic {
			return a
		}
	}
	t.Fatal("no claude account discovered")
	return provider.Account{}
}

// A .claude.json padded to 2 MiB still proves the identity.
func TestClaudeProfileBeyondOneMiBStaysProven(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	writeClaudeFixture(t, s, func(path string) {
		body := `{"oauthAccount":{"accountUuid":"u-1","organizationUuid":"o-1","emailAddress":"me@example.com"},"pad":"` +
			strings.Repeat("x", 2<<20) + `"}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	a := claudeRow(t, s)
	if a.ID != "anthropic:u-1:o-1" || a.Label != "me@example.com" || a.Note != "" {
		t.Errorf("account = id %q label %q note %q, want proven", a.ID, a.Label, a.Note)
	}
}

// A .claude.json over the cap is reported, not treated as absent.
func TestClaudeProfileOverCapIsReported(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	writeClaudeFixture(t, s, func(path string) { sparse(t, path, maxReadFile+1) })
	a := claudeRow(t, s)
	if want := "account profile too large (.claude.json): identity unknown"; a.Note != want {
		t.Errorf("note = %q, want %q", a.Note, want)
	}
	if a.ID != "claude" {
		t.Errorf("id = %q, want the unproven id", a.ID)
	}
}

// A credential file over the cap is unreadable for the default profile.
func TestCredentialFileOverCapIsUnreadable(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	if err := os.MkdirAll(filepath.Join(s.Home(), ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	sparse(t, filepath.Join(s.Home(), ".claude", ".credentials.json"), maxReadFile+1)
	a := claudeRow(t, s)
	if a.Note != "credentials unreadable" {
		t.Errorf("note = %q, want credentials unreadable", a.Note)
	}
}

func TestOSStoreSameFile(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	dir := filepath.Join(s.Home(), "a")
	other := filepath.Join(s.Home(), "b")
	for _, d := range []string{dir, other} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(s.Home(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if !s.SameFile(dir, link) {
		t.Error("a link and its target must be the same file")
	}
	if s.SameFile(dir, other) {
		t.Error("different directories must differ")
	}
	if s.SameFile(dir, filepath.Join(s.Home(), "missing")) {
		t.Error("a missing path is never the same")
	}
}
