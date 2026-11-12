package config

import (
	"path/filepath"
	"slices"
	"strings"
)

// Dirs are the directories agents owns.
type Dirs struct {
	Config string
	Cache  string
	State  string
	Data   string
}

// ResolveDirs resolves the directories from the XDG variables when set (on any
// OS), else the platform default: %APPDATA% and %LOCALAPPDATA% on windows,
// the XDG defaults under home elsewhere (macOS included).
func ResolveDirs(getenv func(string) string, goos, home string) Dirs {
	pick := func(xdg, winEnv, winFallback, winSub, unix string) string {
		if v := getenv(xdg); v != "" {
			return filepath.Join(v, "agents")
		}
		if goos == "windows" {
			base := getenv(winEnv)
			if base == "" {
				base = filepath.Join(home, winFallback)
			}
			return filepath.Join(base, "agents", winSub)
		}
		return filepath.Join(home, unix, "agents")
	}
	return Dirs{
		Config: pick("XDG_CONFIG_HOME", "APPDATA", filepath.Join("AppData", "Roaming"), "", ".config"),
		Cache:  pick("XDG_CACHE_HOME", "LOCALAPPDATA", filepath.Join("AppData", "Local"), "cache", ".cache"),
		State:  pick("XDG_STATE_HOME", "LOCALAPPDATA", filepath.Join("AppData", "Local"), "state", filepath.Join(".local", "state")),
		Data:   pick("XDG_DATA_HOME", "LOCALAPPDATA", filepath.Join("AppData", "Local"), "data", filepath.Join(".local", "share")),
	}
}

// Expand replaces ${VAR} (only that form) with its value and returns the names
// that were unset or empty, in order of first appearance. The text of an unset
// reference stays in the result, which is then unusable: callers skip the entry
// and warn.
func Expand(s string, getenv func(string) string) (string, []string) {
	var (
		b     strings.Builder
		unset []string
	)
	for {
		i := strings.Index(s, "${")
		if i < 0 {
			break
		}
		end := strings.IndexByte(s[i+2:], '}')
		if end < 0 {
			break
		}
		name := s[i+2 : i+2+end]
		b.WriteString(s[:i])
		if !validName(name) {
			b.WriteString(s[i : i+2+end+1])
		} else if v := getenv(name); v != "" {
			b.WriteString(v)
		} else {
			b.WriteString(s[i : i+2+end+1])
			if !slices.Contains(unset, name) {
				unset = append(unset, name)
			}
		}
		s = s[i+2+end+1:]
	}
	b.WriteString(s)
	return b.String(), unset
}

// ExpandPath is Expand with a leading "~/" (or a bare "~") replaced by home.
func ExpandPath(p, home string, getenv func(string) string) (string, []string) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		out, unset := Expand(p[1:], getenv)
		return filepath.Join(home, out), unset
	}
	return Expand(p, getenv)
}

func validName(n string) bool {
	if n == "" {
		return false
	}
	for i, r := range n {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
