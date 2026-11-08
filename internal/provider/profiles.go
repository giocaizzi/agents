package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/usage"
)

// maxGlobProfiles caps the extra profile directories found by listing, per harness.
const maxGlobProfiles = 8

// profileSpec describes where one harness keeps its login profiles: a config
// directory selected by an environment variable (one login per directory), the
// default directory, and well-known sibling directories found by listing a
// parent directory.
type profileSpec struct {
	harness   string
	envVar    string
	def       []string // default directory, relative to home
	listDir   []string // directory listed for sibling profiles, relative to home
	dirPrefix string   // a listed entry is a candidate only if its name starts with this
	prefix    string   // stripped from a directory name to get the profile name
	markers   []string // a listed directory is a profile only if it holds one of these
}

func claudeSpec() profileSpec {
	return profileSpec{harness: harnessClaude, envVar: "CLAUDE_CONFIG_DIR", def: []string{".claude"}, dirPrefix: ".claude-", prefix: "claude-", markers: []string{".credentials.json", ".claude.json"}}
}

func codexSpec() profileSpec {
	return profileSpec{harness: "codex", envVar: "CODEX_HOME", def: []string{".codex"}, dirPrefix: ".codex-", prefix: "codex-", markers: []string{"auth.json", "config.toml"}}
}

func piSpec() profileSpec {
	return profileSpec{harness: "pi", envVar: "PI_CODING_AGENT_DIR", def: []string{".pi", "agent"}, listDir: []string{".pi"}, dirPrefix: "agent-", prefix: "agent-", markers: []string{"auth.json"}}
}

// profile is one login directory of a harness.
type profile struct {
	harness   string
	dir       string // as found: the env value, the default, or the listed directory
	canon     string // dir with symlinks resolved
	name      string // empty for the default directory
	active    bool   // the profile the process environment belongs to
	isDefault bool
	envSet    bool // dir came from the environment variable
}

// label is the provisional harness label, qualified by the profile name:
// "claude:work". join replaces it by the final label.
func (p profile) label() string {
	if p.name == "" {
		return p.harness
	}
	return p.harness + ":" + p.name
}

// pathHash is the sha256 of the canonical directory, in hex.
func (p profile) pathHash() string {
	sum := sha256.Sum256([]byte(p.canon))
	return hex.EncodeToString(sum[:])
}

// qualified is the profile name made unique by its canonical path: "work@1a2b3c4d".
func (p profile) qualified() string {
	if p.name == "" {
		return ""
	}
	return p.name + "@" + p.pathHash()[:8]
}

// id is the account id prefix: the harness, qualified for a named profile.
func (p profile) id() string {
	if p.name == "" {
		return p.harness
	}
	return p.harness + ":" + p.qualified()
}

// keySuffix qualifies a synthetic merge key so two profiles never merge on it.
func (p profile) keySuffix() string {
	if p.name == "" {
		return ""
	}
	return ":" + p.qualified()
}

// explicit reports whether the harness was pointed at dir (environment
// variable) or dir is not the default: the cases where Claude Code hashes the
// directory into its keychain service.
func (p profile) explicit() bool { return p.envSet || !p.isDefault }

// silent reports whether an unreadable credential is skipped without a row: a
// listed profile was never asked for.
func (p profile) silent() bool { return !p.envSet && !p.isDefault }

// qualifyID rewrites the provisional label prefix of account ids to the
// path-qualified one.
func (p profile) qualifyID(accs []Account) []Account {
	prov, id := p.label(), p.id()
	if prov == id {
		return accs
	}
	for i, a := range accs {
		if rest, ok := strings.CutPrefix(a.ID, prov); ok {
			accs[i].ID = id + rest
		}
	}
	return accs
}

func (s profileSpec) profileName(dir string) string {
	base := strings.TrimPrefix(filepath.Base(dir), ".")
	if n := strings.TrimPrefix(base, s.prefix); n != "" {
		return n
	}
	return base
}

func (s profileSpec) homePath(st Store, rel []string) string {
	return filepath.Join(append([]string{st.Home()}, rel...)...)
}

// profiles lists the profiles of a harness: the environment directory, the
// default directory, then up to maxGlobProfiles listed directories that hold
// one of the harness's files. Directories are deduplicated by canonical path;
// the environment directory wins; directories that are the same file count as
// one. A listed directory that resolves to the home directory or above is
// skipped.
func (s profileSpec) profiles(st Store) []profile {
	def := s.homePath(st, s.def)
	var seen []string // canonical directories taken
	taken := func(canon string) bool {
		return slices.ContainsFunc(seen, func(c string) bool { return c == canon || st.SameFile(c, canon) })
	}
	var out []profile

	env := st.Getenv(s.envVar)
	if env != "" {
		p := profile{harness: s.harness, dir: env, canon: st.Canonical(env), active: true, envSet: true}
		if p.canon == st.Canonical(def) {
			p.isDefault = true
		} else {
			p.name = s.profileName(env)
		}
		seen = append(seen, p.canon)
		out = append(out, p)
	}
	if c := st.Canonical(def); !taken(c) {
		seen = append(seen, c)
		out = append(out, profile{harness: s.harness, dir: def, canon: c, isDefault: true, active: env == ""})
	}

	parent := s.homePath(st, s.listDir)
	names := st.ListDir(parent)
	slices.Sort(names)
	listed := 0
	canonHome := st.Canonical(st.Home())
	for _, n := range names {
		if listed == maxGlobProfiles {
			break
		}
		if !strings.HasPrefix(n, s.dirPrefix) {
			continue
		}
		dir := filepath.Join(parent, n)
		canon := st.Canonical(dir)
		if taken(canon) || homeOrAbove(st, canon, canonHome) || !s.hasMarker(st, dir) {
			continue
		}
		seen = append(seen, canon)
		listed++
		out = append(out, profile{harness: s.harness, dir: dir, canon: canon, name: s.profileName(dir)})
	}
	return out
}

// homeOrAbove reports whether dir is home or one of its ancestors, by path or,
// on a case-insensitive file system or through links, as the same directory.
func homeOrAbove(st Store, dir, home string) bool {
	if containsPath(dir, home) {
		return true
	}
	for a := home; ; {
		if st.SameFile(dir, a) {
			return true
		}
		parent := filepath.Dir(a)
		if parent == a {
			return false
		}
		a = parent
	}
}

// containsPath reports whether dir is path or one of its ancestors.
func containsPath(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s profileSpec) hasMarker(st Store, dir string) bool {
	for _, m := range s.markers {
		if st.Exists(filepath.Join(dir, m)) {
			return true
		}
	}
	return false
}

// collect reads every profile of the harness with read and joins the accounts.
func (s profileSpec) collect(st Store, read func(Store, profile) []Account) []Account {
	profiles := s.profiles(st)
	per := make([][]Account, len(profiles))
	for i, p := range profiles {
		per[i] = p.qualifyID(read(st, p))
	}
	return s.join(st, profiles, per)
}

// join concatenates the accounts of each profile and sets their final harness
// label. When exactly one profile produced accounts, names are not shown: its
// label becomes the plain harness name. When two shown profiles share a name,
// the parent directory's base name is prepended to both, and, if they still
// collide, the first 4 hex of the path hash is appended. Account ids stay
// qualified by the path hash. An expired token's note names the final label
// and, for a non-default profile, the directory to open it with.
func (s profileSpec) join(st Store, profiles []profile, per [][]Account) []Account {
	var shown []int
	for i, accs := range per {
		if len(accs) > 0 {
			shown = append(shown, i)
		}
	}
	labels := finalLabels(s.harness, profiles, shown)

	var out []Account
	for _, i := range shown {
		p := profiles[i]
		for _, a := range per[i] {
			a.Harnesses = slices.Clone(a.Harnesses)
			for j, h := range a.Harnesses {
				if h == p.label() {
					a.Harnesses[j] = labels[i]
				}
			}
			if a.Status == usage.StatusExpired {
				a.Note = s.expiredNote(st, p, labels[i])
			}
			out = append(out, a)
		}
	}
	return out
}

// expiredNote tells the user how to open the harness on the profile.
func (s profileSpec) expiredNote(st Store, p profile, label string) string {
	switch {
	case !p.isDefault:
		return "token expired, open " + label + " (" + s.envVar + "=" + shellPath(p.dir, st.Home()) + ") to refresh"
	case !p.envSet && st.Getenv(s.envVar) != "":
		// The variable points at another profile: the plain command would open that one.
		return "token expired, open " + label + " with " + s.envVar + " unset to refresh"
	}
	return "token expired, open " + label + " to refresh"
}

// shellPath is tildePath quoted for a POSIX shell when needed. A leading "~/"
// stays outside the quotes so the shell still expands it.
func shellPath(path, home string) string {
	t := tildePath(path, home)
	if rest, ok := strings.CutPrefix(t, "~/"); ok {
		return "~/" + shellQuote(rest)
	}
	if t == "~" {
		return t
	}
	return shellQuote(t)
}

// shellQuote single-quotes s unless it only holds characters safe in a word.
func shellQuote(s string) string {
	safe := func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./~@%+=:,-", r)
	}
	if s != "" && !strings.ContainsFunc(s, func(r rune) bool { return !safe(r) }) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// tildePath writes a path under home with a leading "~".
func tildePath(path, home string) string {
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return "~/" + filepath.ToSlash(rel)
}

// finalLabels returns the display label of each shown profile, by index.
func finalLabels(harness string, profiles []profile, shown []int) map[int]string {
	labels := make(map[int]string, len(shown))
	if len(shown) == 1 {
		labels[shown[0]] = harness
		return labels
	}
	names := make(map[int]string, len(shown))
	for _, i := range shown {
		names[i] = profiles[i].name
	}
	disambiguate(names, func(i int) string {
		parent := filepath.Base(filepath.Dir(profiles[i].dir))
		if parent == "." || parent == string(filepath.Separator) {
			return ""
		}
		return parent + "/" + profiles[i].name
	})
	disambiguate(names, func(i int) string { return names[i] + "@" + profiles[i].pathHash()[:4] })
	for _, i := range shown {
		labels[i] = harness
		if names[i] != "" {
			labels[i] = harness + ":" + names[i]
		}
	}
	return labels
}

// disambiguate replaces, with qualify, every non-empty name shared by two or
// more entries. A qualify result of "" keeps the name.
func disambiguate(names map[int]string, qualify func(i int) string) {
	count := map[string]int{}
	for _, n := range names {
		if n != "" {
			count[n]++
		}
	}
	for i, n := range names {
		if n != "" && count[n] > 1 {
			if q := qualify(i); q != "" {
				names[i] = q
			}
		}
	}
}
