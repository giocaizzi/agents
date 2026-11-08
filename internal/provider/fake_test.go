package provider

import (
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// fakeStore is an in-memory Store.
type fakeStore struct {
	home    string
	goos    string
	now     time.Time
	files   map[string]string
	secrets map[string]string // service + "\x00" + account
	env     map[string]string
	canon   map[string]string        // Canonical mapping
	alias   map[string]string        // canonical path -> the path SameFile treats it as
	rows    map[string][]OpencodeRow // by database path
	rowsErr error                    // returned for any database not in rows, instead of ErrNotExist
}

var testNow = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func newFake(goos string) *fakeStore {
	return &fakeStore{
		home:    filepath.FromSlash("/home/user"),
		goos:    goos,
		now:     testNow,
		files:   map[string]string{},
		secrets: map[string]string{},
		env:     map[string]string{},
		rows:    map[string][]OpencodeRow{},
	}
}

func (f *fakeStore) path(elem ...string) string {
	return filepath.Join(append([]string{f.home}, elem...)...)
}

func (f *fakeStore) ReadFile(p string) ([]byte, error) {
	if s, ok := f.files[p]; ok {
		return []byte(s), nil
	}
	return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
}

func (f *fakeStore) Secret(service, account string) (string, error) {
	if s, ok := f.secrets[service+"\x00"+account]; ok {
		return s, nil
	}
	return "", fs.ErrNotExist
}

func (f *fakeStore) OpencodeCredentials(db string) ([]OpencodeRow, error) {
	if r, ok := f.rows[db]; ok {
		return r, nil
	}
	if f.rowsErr != nil {
		return nil, f.rowsErr
	}
	return nil, &fs.PathError{Op: "open", Path: db, Err: fs.ErrNotExist}
}

// Canonical returns the path unchanged unless canon maps it.
func (f *fakeStore) Canonical(p string) string {
	if c, ok := f.canon[p]; ok {
		return c
	}
	return p
}

// SameFile compares the canonical paths, after the alias mapping.
func (f *fakeStore) SameFile(a, b string) bool {
	norm := func(p string) string {
		c := f.Canonical(p)
		if al, ok := f.alias[c]; ok {
			return al
		}
		return c
	}
	return norm(a) == norm(b)
}

func (f *fakeStore) Exists(p string) bool {
	if _, ok := f.files[p]; ok {
		return true
	}
	for k := range f.files {
		if strings.HasPrefix(k, p+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// ListDir returns the names of the entries directly under dir, derived from
// the file paths.
func (f *fakeStore) ListDir(dir string) []string {
	return listFakeDir(f.files, dir)
}

// listFakeDir returns the sorted first path components under dir.
func listFakeDir(files map[string]string, dir string) []string {
	seen := map[string]bool{}
	var out []string
	prefix := dir + string(filepath.Separator)
	for k := range files {
		rest, ok := strings.CutPrefix(k, prefix)
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

func (f *fakeStore) Getenv(k string) string { return f.env[k] }
func (f *fakeStore) Home() string           { return f.home }
func (f *fakeStore) GOOS() string           { return f.goos }
func (f *fakeStore) Now() time.Time         { return f.now }

// jwt builds an unsigned fake JWT with the given claims.
func jwt(t *testing.T, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]string{"alg": "none"}) + "." + enc(claims) + ".fake-signature"
}

// view is the comparable projection of an Account; Plan is covered separately
// because the plan mappings belong to package usage.
type view struct {
	Provider  string
	Label     string
	Key       string
	Harnesses []string
	Auth      Auth
	Status    string
	Note      string
	Login     string
	Token     string
	AccountID string
	Expires   time.Time
	Source    string
}

func views(accs []Account) []view {
	out := make([]view, 0, len(accs))
	for _, a := range accs {
		out = append(out, view{
			Provider: a.Provider, Label: a.Label, Key: a.Key, Harnesses: a.Harnesses,
			Auth: a.Auth, Status: string(a.Status), Note: a.Note, Login: a.Login,
			Token: a.Cred.Token(), AccountID: a.Cred.AccountID(), Expires: a.Expires, Source: a.Source,
		})
	}
	return out
}

func assertViews(t *testing.T, got []Account, want []view) {
	t.Helper()
	if diff := cmp.Diff(want, views(got), cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("accounts mismatch (-want +got):\n%s", diff)
	}
}
