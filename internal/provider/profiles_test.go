package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// recordStore records every ReadFile path and fails the paths in fail.
type recordStore struct {
	*fakeStore

	reads []string
	fail  map[string]bool
}

func (r *recordStore) ReadFile(p string) ([]byte, error) {
	r.reads = append(r.reads, p)
	if r.fail[p] {
		return nil, errors.New("permission denied")
	}
	return r.fakeStore.ReadFile(p)
}

func claudeCredsFor(token, subscription string, exp time.Time) string {
	return fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d,"subscriptionType":%q}}`, token, exp.UnixMilli(), subscription)
}

func claudeAccountJSON(uuid, org string) string {
	return fmt.Sprintf(`{"oauthAccount":{"accountUuid":%q,"emailAddress":%q,"organizationUuid":%q}}`, uuid, uuid+"@example.com", org)
}

// rows summarises accounts as "harnesses key".
func rows(accs []Account) []string {
	out := make([]string, 0, len(accs))
	for _, a := range accs {
		out = append(out, strings.Join(a.Harnesses, ",")+" "+a.Key)
	}
	return out
}

// pathHash8 is the first 8 hex of the sha256 of a canonical path.
func pathHash8(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])[:8]
}

func claudeHashed(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return claudeKeychainService + "-" + hex.EncodeToString(sum[:])[:8]
}

func TestClaudeProfiles(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	one, two := "anthropic:u-1:o-1", "anthropic:u-2:o-2"
	tests := []struct {
		name  string
		setup func(f *fakeStore)
		want  []string
	}{
		{
			name: "only default",
			setup: func(f *fakeStore) {
				f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", exp)
				f.files[f.path(".claude.json")] = claudeAccountJSON("u-1", "o-1")
			},
			want: []string{"claude " + one},
		},
		{
			name: "only a globbed profile, no default",
			setup: func(f *fakeStore) {
				f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", exp)
				f.files[f.path(".claude-work", ".claude.json")] = claudeAccountJSON("u-2", "o-2")
			},
			want: []string{"claude " + two}, // one profile: names are not shown
		},
		{
			name: "default and work, different identities",
			setup: func(f *fakeStore) {
				f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", exp)
				f.files[f.path(".claude.json")] = claudeAccountJSON("u-1", "o-1")
				f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "enterprise", exp)
				f.files[f.path(".claude-work", ".claude.json")] = claudeAccountJSON("u-2", "o-2")
			},
			want: []string{"claude " + one, "claude:work " + two},
		},
		{
			name: "default and work, same identity",
			setup: func(f *fakeStore) {
				f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", exp)
				f.files[f.path(".claude.json")] = claudeAccountJSON("u-1", "o-1")
				f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", exp)
				f.files[f.path(".claude-work", ".claude.json")] = claudeAccountJSON("u-1", "o-1")
			},
			want: []string{"claude,claude:work " + one},
		},
		{
			name: "env dir is a glob profile too: no duplicate",
			setup: func(f *fakeStore) {
				f.env["CLAUDE_CONFIG_DIR"] = f.path(".claude-work")
				f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", exp)
				f.files[f.path(".claude.json")] = claudeAccountJSON("u-1", "o-1")
				f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", exp)
				f.files[f.path(".claude-work", ".claude.json")] = claudeAccountJSON("u-2", "o-2")
			},
			want: []string{"claude " + one, "claude:work " + two},
		},
		{
			name: "env dir that is the default dir is the default",
			setup: func(f *fakeStore) {
				f.env["CLAUDE_CONFIG_DIR"] = f.path(".claude")
				f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", exp)
				f.files[f.path(".claude", ".claude.json")] = claudeAccountJSON("u-1", "o-1")
			},
			want: []string{"claude " + one},
		},
		{
			name: "directory without markers is ignored",
			setup: func(f *fakeStore) {
				f.files[f.path(".claude-backup", "notes.txt")] = "fake-token-in-notes"
				f.files[f.path(".claude-backup", "settings.json")] = `{"env":{"CLAUDE_CODE_USE_BEDROCK":"1"}}`
				f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", exp)
				f.files[f.path(".claude-work", ".claude.json")] = claudeAccountJSON("u-2", "o-2")
			},
			want: []string{"claude " + two},
		},
		{
			name: "symlinked glob dir of the default is deduped, default wins",
			setup: func(f *fakeStore) {
				f.canon = map[string]string{f.path(".claude-main"): f.path(".claude")}
				for _, d := range []string{".claude", ".claude-main"} {
					f.files[f.path(d, ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", exp)
					f.files[f.path(d, ".claude.json")] = claudeAccountJSON("u-1", "o-1")
				}
				f.files[f.path(".claude.json")] = claudeAccountJSON("u-1", "o-1")
			},
			want: []string{"claude " + one},
		},
		{
			name: "two symlinks of one dir count once",
			setup: func(f *fakeStore) {
				f.canon = map[string]string{f.path(".claude-a"): f.path(".claude-real"), f.path(".claude-b"): f.path(".claude-real")}
				for _, d := range []string{".claude-a", ".claude-b"} {
					f.files[f.path(d, ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", exp)
				}
			},
			want: []string{"claude " + fingerprint(Anthropic, "fake-tok-w")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake("linux")
			tt.setup(f)
			if diff := cmp.Diff(tt.want, rows(Merge(readClaude(f))), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("rows (-want +got):\n%s", diff)
			}
		})
	}
}

func TestClaudeProfileIDsStayQualified(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", testNow.Add(time.Hour))
	got := readClaude(f)
	want := "claude:work@" + pathHash8(f.path(".claude-work"))
	if len(got) != 1 || got[0].ID != want || strings.Join(got[0].Harnesses, ",") != "claude" {
		t.Errorf("id %q harnesses %v, want id %s and harness claude", got[0].ID, got[0].Harnesses, want)
	}
}

func TestClaudeExpiredNoteNamesProfile(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", testNow.Add(time.Hour))
	f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", testNow.Add(-time.Hour))
	var note string
	for _, a := range readClaude(f) {
		if a.Status != "" {
			note = a.Note
		}
	}
	if want := "token expired, open claude:work (CLAUDE_CONFIG_DIR=~/.claude-work) to refresh"; note != want {
		t.Errorf("note = %q, want %q", note, want)
	}
}

func TestClaudeGlobbedFilesAreReadOnlyAfterMarker(t *testing.T) {
	t.Parallel()
	f := &recordStore{fakeStore: newFake("linux")}
	f.files[f.path(".claude-backup", "notes.txt")] = "fake-token-in-notes"
	f.files[f.path(".claude-backup", "settings.json")] = "{}"
	readClaude(f)
	for _, p := range f.reads {
		if strings.Contains(p, ".claude-backup") {
			t.Errorf("read %s in a directory without markers", p)
		}
	}
}

func TestProfilesCap(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	for i := range 12 {
		f.files[f.path(fmt.Sprintf(".claude-p%02d", i), ".credentials.json")] = claudeCredsFor(fmt.Sprintf("fake-tok-%d", i), "max", testNow.Add(time.Hour))
	}
	got := rows(Merge(readClaude(f)))
	if len(got) != maxGlobProfiles {
		t.Fatalf("got %d rows, want %d: %v", len(got), maxGlobProfiles, got)
	}
	for _, r := range got {
		for _, late := range []string{"p08", "p09", "p10", "p11"} {
			if strings.Contains(r, ":"+late+" ") {
				t.Errorf("row %q is past the cap", r)
			}
		}
	}
}

func TestClaudeDarwinProfiles(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	t.Run("keychain service hashes each globbed dir", func(t *testing.T) {
		t.Parallel()
		f := newFake("darwin")
		work := f.path(".claude-work")
		f.files[work+"/.claude.json"] = claudeAccountJSON("u-2", "o-2")
		f.secrets[claudeKeychainService+"\x00"] = claudeCredsFor("fake-tok-d", "max", exp)
		f.secrets[claudeHashed(work)+"\x00"] = claudeCredsFor("fake-tok-w", "max", exp)
		got := readClaude(f)
		byToken := map[string]Account{}
		for _, a := range got {
			byToken[a.Cred.Token()] = a
		}
		if len(got) != 2 || byToken["fake-tok-w"].Harnesses[0] != "claude:work" || byToken["fake-tok-w"].Source != srcKeychain ||
			byToken["fake-tok-d"].Harnesses[0] != "claude" {
			t.Errorf("got %v", rows(got))
		}
	})
	t.Run("file fallback when the keychain has no item", func(t *testing.T) {
		t.Parallel()
		f := newFake("darwin")
		f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", exp)
		got := readClaude(f)
		if len(got) != 1 || got[0].Cred.Token() != "fake-tok-w" || got[0].Source != srcFile {
			t.Errorf("got %v", rows(got))
		}
	})
	t.Run("keychain wins over the file", func(t *testing.T) {
		t.Parallel()
		f := newFake("darwin")
		f.secrets[claudeKeychainService+"\x00"] = claudeCredsFor("fake-tok-k", "max", exp)
		f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-f", "max", exp)
		got := readClaude(f)
		if len(got) != 1 || got[0].Cred.Token() != "fake-tok-k" {
			t.Errorf("got %v", rows(got))
		}
	})
}

func TestEnvOnlyModesBelongToTheActiveProfile(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	tests := []struct {
		name     string
		envDir   string // relative to home; empty = unset
		wantAuth map[string]Auth
	}{
		{"no env dir: the default is active", "", map[string]Auth{"claude": AuthCloud, "claude:work": AuthOAuth}},
		{"env dir: that profile is active", ".claude-work", map[string]Auth{"claude": AuthOAuth, "claude:work": AuthCloud}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake("linux")
			f.env["CLAUDE_CODE_USE_BEDROCK"] = "1"
			if tt.envDir != "" {
				f.env["CLAUDE_CONFIG_DIR"] = f.path(tt.envDir)
			}
			f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", exp)
			f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", exp)
			got := map[string]Auth{}
			for _, a := range readClaude(f) {
				got[a.Harnesses[0]] = a.Auth
			}
			if diff := cmp.Diff(tt.wantAuth, got); diff != "" {
				t.Errorf("auth by harness (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("a profile's own settings env applies to it alone", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", exp)
		f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", exp)
		f.files[f.path(".claude-work", "settings.json")] = `{"env":{"CLAUDE_CODE_USE_VERTEX":"1"}}`
		got := map[string]Auth{}
		for _, a := range readClaude(f) {
			got[a.Harnesses[0]] = a.Auth
		}
		if diff := cmp.Diff(map[string]Auth{"claude": AuthOAuth, "claude:work": AuthCloud}, got); diff != "" {
			t.Errorf("auth by harness (-want +got):\n%s", diff)
		}
	})
}

func TestClaudeUnreadableCredentials(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	build := func() *recordStore {
		f := &recordStore{fakeStore: newFake("linux"), fail: map[string]bool{}}
		f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", exp)
		f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", exp)
		return f
	}
	t.Run("globbed is skipped silently", func(t *testing.T) {
		t.Parallel()
		f := build()
		f.fail[f.path(".claude-work", ".credentials.json")] = true
		got := readClaude(f)
		if len(got) != 1 || got[0].Cred.Token() != "fake-tok-d" || got[0].Note != "" {
			t.Errorf("got %v note %q", rows(got), got[0].Note)
		}
	})
	t.Run("globbed malformed is skipped silently", func(t *testing.T) {
		t.Parallel()
		f := build()
		f.files[f.path(".claude-work", ".credentials.json")] = "{not json"
		if got := readClaude(f); len(got) != 1 {
			t.Errorf("got %v", rows(got))
		}
	})
	t.Run("default shows unreadable", func(t *testing.T) {
		t.Parallel()
		f := build()
		f.fail[f.path(".claude", ".credentials.json")] = true
		got := readClaude(f)
		notes := make([]string, 0, len(got))
		for _, a := range got {
			notes = append(notes, a.Harnesses[0]+":"+a.Note)
		}
		if diff := cmp.Diff([]string{"claude:credentials unreadable", "claude:work:"}, notes); diff != "" {
			t.Errorf("notes (-want +got):\n%s", diff)
		}
	})
	t.Run("env dir shows unreadable", func(t *testing.T) {
		t.Parallel()
		f := build()
		f.env["CLAUDE_CONFIG_DIR"] = f.path(".claude-work")
		f.fail[f.path(".claude-work", ".credentials.json")] = true
		var unreadableRows int
		for _, a := range readClaude(f) {
			if a.Note == noteUnreadable {
				unreadableRows++
			}
		}
		if unreadableRows != 1 {
			t.Errorf("unreadable rows = %d, want 1", unreadableRows)
		}
	})
}

func TestCodexProfiles(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	t.Run("default and work, different accounts", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		a1, i1 := chatgptTokens(t, exp, "acct-1")
		a2, i2 := chatgptTokens(t, exp, "acct-2")
		f.files[f.path(".codex", "auth.json")] = codexAuthJSON(t, a1, i1, "acct-1")
		f.files[f.path(".codex-work", "auth.json")] = codexAuthJSON(t, a2, i2, "acct-2")
		want := []string{"codex openai:acct-1:user-1", "codex:work openai:acct-2:user-1"}
		if diff := cmp.Diff(want, rows(Merge(readCodex(f)))); diff != "" {
			t.Errorf("rows (-want +got):\n%s", diff)
		}
	})
	t.Run("darwin keychain account per home", func(t *testing.T) {
		t.Parallel()
		f := newFake("darwin")
		a1, i1 := chatgptTokens(t, exp, "acct-1")
		a2, i2 := chatgptTokens(t, exp, "acct-2")
		work := f.path(".codex-work")
		f.files[work+"/config.toml"] = "model = \"x\""
		f.secrets[codexKeyringService+"\x00"+codexStoreKey(f.path(".codex"))] = codexAuthJSON(t, a1, i1, "acct-1")
		f.secrets[codexKeyringService+"\x00"+codexStoreKey(work)] = codexAuthJSON(t, a2, i2, "acct-2")
		got := Merge(readCodex(f))
		if diff := cmp.Diff([]string{"codex openai:acct-1:user-1", "codex:work openai:acct-2:user-1"}, rows(got)); diff != "" {
			t.Errorf("rows (-want +got):\n%s", diff)
		}
		for _, a := range got {
			if a.Source != srcKeychain {
				t.Errorf("source = %q", a.Source)
			}
		}
	})
	t.Run("OPENAI_API_KEY belongs to the active profile", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.env["OPENAI_API_KEY"] = "fake-key"
		f.files[f.path(".codex-work", "config.toml")] = "x"
		a1, i1 := chatgptTokens(t, exp, "acct-1")
		f.files[f.path(".codex-work", "auth.json")] = codexAuthJSON(t, a1, i1, "acct-1")
		got := readCodex(f)
		if len(got) != 2 {
			t.Fatalf("got %v", rows(got))
		}
		for _, a := range got {
			if a.Auth == AuthAPIKey && a.Harnesses[0] != "codex" {
				t.Errorf("api key row on %v", a.Harnesses)
			}
		}
	})
	t.Run("unreadable globbed is skipped", func(t *testing.T) {
		t.Parallel()
		f := &recordStore{fakeStore: newFake("linux"), fail: map[string]bool{}}
		f.files[f.path(".codex-work", "auth.json")] = "{}"
		f.fail[f.path(".codex-work", "auth.json")] = true
		if got := readCodex(f); len(got) != 0 {
			t.Errorf("got %v", rows(got))
		}
	})
}

func TestPiProfiles(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	f := newFake("linux")
	a1 := identTokens(t, exp, "acct-1", "user-1")
	a2 := identTokens(t, exp, "acct-2", "user-2")
	f.files[f.path(".pi", "agent", "auth.json")] = piCodexFile(a1, exp, "acct-1")
	f.files[f.path(".pi", "agent-work", "auth.json")] = piCodexFile(a2, exp, "acct-2")
	f.files[f.path(".pi", "agent-empty", "models.json")] = "{}"
	got := Merge(readPi(f))
	want := []string{"pi openai:acct-1:user-1", "pi:work openai:acct-2:user-2"}
	if diff := cmp.Diff(want, rows(got)); diff != "" {
		t.Errorf("rows (-want +got):\n%s", diff)
	}

	t.Run("unproven id is qualified", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.files[f.path(".pi", "agent-work", "auth.json")] = `{"anthropic":{"type":"oauth","access":"fake-pi-tok"}}`
		got := readPi(f)
		want := "pi:work@" + pathHash8(f.path(".pi", "agent-work")) + ":anthropic"
		if len(got) != 1 || got[0].ID != want || got[0].Harnesses[0] != "pi" {
			t.Errorf("got id %q harnesses %v, want id %s", got[0].ID, got[0].Harnesses, want)
		}
	})
}
