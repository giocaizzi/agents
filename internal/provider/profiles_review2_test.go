package provider

import (
	"testing"
	"time"
)

// A listed directory that canonicalises to the home directory in another case
// is the home directory, and is skipped.
func TestCaseVariantOfHomeIsSkipped(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	caseDir := f.path(".claude-case")
	f.canon = map[string]string{caseDir: "/HOME/USER"}
	f.alias = map[string]string{"/HOME/USER": f.home}
	f.files[f.path(".claude-case", ".credentials.json")] = claudeCredsFor("fake-tok-c", "max", testNow.Add(time.Hour))
	if got := readClaude(f); len(got) != 0 {
		t.Errorf("accounts = %v, want the case variant of home skipped", rows(got))
	}
}

// Directories that are the same file count as one profile.
func TestSameFileDirectoriesDedupe(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.canon = map[string]string{f.path(".claude-work"): "/DATA/Work"}
	f.alias = map[string]string{"/DATA/Work": f.path(".claude-other")}
	f.files[f.path(".claude-other", ".credentials.json")] = claudeCredsFor("fake-tok-o", "max", testNow.Add(time.Hour))
	f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", testNow.Add(time.Hour))
	if got := readClaude(f); len(got) != 1 {
		t.Errorf("accounts = %v, want one row for the same directory", rows(got))
	}
}

func TestExpiredDefaultNoteWhenEnvPointsElsewhere(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.env["CLAUDE_CONFIG_DIR"] = f.path(".claude-work")
	f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", testNow.Add(-time.Hour))
	f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", testNow.Add(time.Hour))
	var note string
	for _, a := range readClaude(f) {
		if a.Status != "" {
			note = a.Note
		}
	}
	if want := "token expired, open claude with CLAUDE_CONFIG_DIR unset to refresh"; note != want {
		t.Errorf("note = %q, want %q", note, want)
	}
}

func TestExpiredDefaultNoteWithoutEnv(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", testNow.Add(-time.Hour))
	got := readClaude(f)
	if len(got) != 1 || got[0].Note != "token expired, open claude to refresh" {
		t.Errorf("accounts = %+v, want the plain note", got)
	}
}

func TestExpiredNoteCodexDefault(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	spec := codexSpec()
	def := profile{harness: "codex", dir: f.path(".codex"), isDefault: true}
	if got, want := spec.expiredNote(f, def, "codex"), "token expired, open codex to refresh"; got != want {
		t.Errorf("no env: %q, want %q", got, want)
	}
	f.env["CODEX_HOME"] = f.path(".codex-x")
	if got, want := spec.expiredNote(f, def, "codex"), "token expired, open codex with CODEX_HOME unset to refresh"; got != want {
		t.Errorf("env elsewhere: %q, want %q", got, want)
	}
	def.envSet = true // the variable points at the default itself
	if got, want := spec.expiredNote(f, def, "codex"), "token expired, open codex to refresh"; got != want {
		t.Errorf("env at default: %q, want %q", got, want)
	}
}

func TestShellPath(t *testing.T) {
	t.Parallel()
	const home = "/home/user"
	for name, tc := range map[string]struct{ path, want string }{
		"plain under home": {"/home/user/.claude-work", "~/.claude-work"},
		"plain outside":    {"/srv/a_b/c-d", "/srv/a_b/c-d"},
		"space under home": {"/home/user/My Dir", "~/'My Dir'"},
		"space outside":    {"/srv/My Dir", "'/srv/My Dir'"},
		"quote under home": {"/home/user/it's", `~/'it'\''s'`},
		"home itself":      {"/home/user", "~"},
		"metachar":         {"/home/user/a$b", "~/'a$b'"},
	} {
		if got := shellPath(tc.path, home); got != tc.want {
			t.Errorf("%s: shellPath(%q) = %q, want %q", name, tc.path, got, tc.want)
		}
	}
}
