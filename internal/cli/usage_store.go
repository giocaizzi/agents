package cli

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver

	"github.com/giocaizzi/agents/internal/provider"
)

const secretTimeout = 5 * time.Second

// currentUser is the OS user whose keychain item comes first: apps write
// their items under it, and a stale item of another account can share the
// service name.
func currentUser(getenv func(string) string) string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return getenv("USER")
}

// osStore is the real provider.Store: files, the OS credential store and
// opencode's SQLite database, all read-only and into memory.
type osStore struct {
	getenv func(string) string
	goos   string
	home   string
	now    func() time.Time

	securityBin string        // the macOS security tool; tests stub it
	user        func() string // the current OS user name; tests stub it
	timeout     time.Duration // per credential read; zero means secretTimeout
}

// limit is the per-read timeout.
func (s osStore) limit() time.Duration {
	if s.timeout > 0 {
		return s.timeout
	}
	return secretTimeout
}

// newOSStore builds the store from the CLI dependencies.
func newOSStore(d Deps) osStore {
	return osStore{
		getenv: d.Getenv, goos: d.GOOS, home: d.home(), now: time.Now, securityBin: "/usr/bin/security", timeout: secretTimeout,
		user: func() string { return currentUser(d.Getenv) },
	}
}

var _ provider.Store = osStore{}

// maxReadFile caps what ReadFile reads. Credential files are a few KiB, but
// Claude Code's .claude.json grows with every project it has seen.
const maxReadFile = 64 << 20

var errNotRegular = errors.New("not a regular file")

// ReadFile reads a regular file of at most maxReadFile bytes. A FIFO or device
// is opened without blocking and rejected; errors carry no file content.
func (s osStore) ReadFile(path string) ([]byte, error) {
	return readRegular(path)
}

// readRegular opens path without blocking, requires a regular file and reads
// it up to maxReadFile bytes. A longer file returns provider.ErrTooLarge.
func readRegular(path string) ([]byte, error) {
	f, err := openNoBlock(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("read file: %w", errNotRegular)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxReadFile+1))
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if len(b) > maxReadFile {
		return nil, fmt.Errorf("read file: %w", provider.ErrTooLarge)
	}
	return b, nil
}

func (s osStore) Exists(path string) bool {
	_, err := os.Stat(filepath.Clean(path))
	return err == nil
}

// SameFile reports whether two paths are the same file or directory; false when
// either cannot be read.
func (s osStore) SameFile(a, b string) bool {
	fa, err := os.Stat(filepath.Clean(a))
	if err != nil {
		return false
	}
	fb, err := os.Stat(filepath.Clean(b))
	return err == nil && os.SameFile(fa, fb)
}

// Canonical resolves symlinks in the absolute path; when that fails it returns
// the cleaned path as given.
func (s osStore) Canonical(path string) string {
	clean := filepath.Clean(path)
	abs, err := filepath.Abs(clean)
	if err != nil {
		return clean
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return clean
	}
	return resolved
}

// ListDir lists the sorted entry names of a directory; nil on error.
func (s osStore) ListDir(dir string) []string {
	entries, err := os.ReadDir(filepath.Clean(dir))
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func (s osStore) Getenv(key string) string { return s.getenv(key) }
func (s osStore) Home() string             { return s.home }
func (s osStore) GOOS() string             { return s.goos }
func (s osStore) Now() time.Time           { return s.now() }

// Secret reads one credential-store item. An empty account means "any account":
// only macOS supports it, through the security tool. Errors never carry the
// secret.
func (s osStore) Secret(service, account string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.limit())
	defer cancel()

	if account == "" {
		if s.goos != "darwin" {
			return "", fmt.Errorf("credential %q: an account is required on %s", service, s.goos)
		}
		acct := ""
		if s.user != nil {
			acct = s.user()
		}
		return s.securityFindPassword(ctx, service, acct)
	}

	type result struct {
		val string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := keyring.Get(service, account)
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return "", fmt.Errorf("credential %q: %w", service, r.err)
		}
		return r.val, nil
	case <-ctx.Done():
		return "", fmt.Errorf("credential %q: timed out", service)
	}
}

// securityItemNotFound is the security tool's exit status for a missing item
// (errSecItemNotFound).
const securityItemNotFound = 44

// securityFindPassword reads the item of the given account first (empty skips
// that step), then any item of the service. The service-only fallback runs only
// when the account's item does not exist: a denied prompt or any other failure
// must not return another account's stale item.
func (s osStore) securityFindPassword(ctx context.Context, service, account string) (string, error) {
	if account != "" {
		out, err := exec.CommandContext(ctx, s.securityBin, "find-generic-password", "-s", service, "-a", account, "-w").Output()
		if err == nil {
			return decodeKeyringValue(strings.TrimSpace(string(out))), nil
		}
		if ee, ok := errors.AsType[*exec.ExitError](err); !ok || ee.ExitCode() != securityItemNotFound {
			// Only the exit status is known here; stderr is never echoed.
			return "", fmt.Errorf("credential %q: not readable", service)
		}
	}
	out, err := exec.CommandContext(ctx, s.securityBin, "find-generic-password", "-s", service, "-w").Output()
	if err != nil {
		// Only the exit status is known here; stderr is never echoed.
		return "", fmt.Errorf("credential %q: not found", service)
	}
	return decodeKeyringValue(strings.TrimSpace(string(out))), nil
}

// decodeKeyringValue undoes go-keyring's storage encodings on macOS.
func decodeKeyringValue(v string) string {
	const (
		b64Prefix = "go-keyring-base64:"
		encPrefix = "go-keyring-encoded:"
	)
	switch {
	case strings.HasPrefix(v, b64Prefix):
		if b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(v, b64Prefix)); err == nil {
			return string(b)
		}
	case strings.HasPrefix(v, encPrefix):
		if b, err := hex.DecodeString(strings.TrimPrefix(v, encPrefix)); err == nil {
			return string(b)
		}
	}
	return v
}

// sqliteDSN is the read-only file: URI of a database path. The path is
// URI-escaped, so ?, # and % in it stay part of the file name. On Windows a
// drive path gets the leading slash a file: URI needs.
func sqliteDSN(path string) string {
	q := url.Values{}
	q.Set("mode", "ro")
	q.Set("_pragma", "busy_timeout(2000)")
	u := url.URL{Scheme: "file", Path: sqlitePath(path), RawQuery: q.Encode()}
	return u.String()
}

// sqlitePath is the URL path of a file path: slashes, and a leading slash
// before a Windows drive letter.
func sqlitePath(path string) string {
	p := filepath.ToSlash(path)
	if len(p) >= 2 && p[1] == ':' {
		p = "/" + p
	}
	return p
}

// OpencodeCredentials reads opencode's V2 credential table, read-only.
func (s osStore) OpencodeCredentials(dbPath string) ([]provider.OpencodeRow, error) {
	path := filepath.Clean(dbPath)
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("opencode database: %w", err)
	}
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open opencode database: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), s.limit())
	defer cancel()
	rows, err := db.QueryContext(ctx, `SELECT id, integration_id, label, active, value FROM credential`)
	if err != nil {
		return nil, fmt.Errorf("query opencode credentials: %w", err)
	}
	defer rows.Close()

	var out []provider.OpencodeRow
	for rows.Next() {
		var (
			id, integration, label sql.NullString
			active                 sql.NullBool
			value                  []byte
		)
		if err := rows.Scan(&id, &integration, &label, &active, &value); err != nil {
			return nil, fmt.Errorf("read opencode credential: %w", err)
		}
		out = append(out, provider.OpencodeRow{
			ID:            id.String,
			IntegrationID: integration.String,
			Label:         label.String,
			Active:        active.Bool,
			Value:         value,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read opencode credentials: %w", err)
	}
	return out, nil
}
