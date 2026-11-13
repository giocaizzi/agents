package provider

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// labelsOf lists the first harness of each account, sorted.
func labelsOf(accs []Account) []string {
	out := make([]string, 0, len(accs))
	for _, a := range accs {
		out = append(out, a.Harnesses[0])
	}
	slices.Sort(out)
	return out
}

func distinct(vals []string) bool {
	s := slices.Clone(vals)
	slices.Sort(s)
	return len(slices.Compact(s)) == len(vals)
}

func TestBedrockAndVertexProfilesOfOneNameStayTwoRows(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	{
		f := newFake("linux")
		env := f.path("proj", ".claude-work")
		f.env["CLAUDE_CONFIG_DIR"] = env
		f.env["CLAUDE_CODE_USE_BEDROCK"] = "1"
		f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", exp)
		f.files[f.path(".claude-work", "settings.json")] = `{"env":{"CLAUDE_CODE_USE_VERTEX":"1"}}`

		got := Discover(f)

		if len(got) != 2 {
			t.Fatalf("got %d accounts, want 2 (bedrock and vertex): %v", len(got), rows(got))
		}
		want := []string{"claude:proj/work", "claude:user/work"}
		if diff := cmp.Diff(want, labelsOf(got)); diff != "" {
			t.Errorf("labels (-want +got):\n%s", diff)
		}
		plans := map[string]string{}
		for _, a := range got {
			plans[a.Harnesses[0]] = a.Plan.Name
		}
		if plans["claude:proj/work"] != "Bedrock" || plans["claude:user/work"] != "Vertex AI" {
			t.Errorf("plans = %v", plans)
		}
		if !distinct([]string{got[0].ID, got[1].ID}) || !distinct([]string{got[0].Key, got[1].Key}) {
			t.Errorf("ids %q %q keys %q %q are not distinct", got[0].ID, got[1].ID, got[0].Key, got[1].Key)
		}
	}
}

func TestSameNameProfilesKeepDistinctRowsAndLabels(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)

	t.Run("env dir outside home vs listed profile", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.env["CLAUDE_CONFIG_DIR"] = filepath.FromSlash("/x/work")
		f.env["CLAUDE_CODE_USE_BEDROCK"] = "1"
		f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", exp)

		got := Discover(f)

		if diff := cmp.Diff([]string{"claude:user/work", "claude:x/work"}, labelsOf(got)); diff != "" {
			t.Errorf("labels (-want +got):\n%s", diff)
		}
	})

	t.Run("same parent base name appends the path hash", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.home = filepath.FromSlash("/b/proj") // the home's base name is the other parent's too
		other := filepath.FromSlash("/a/proj/.claude-work")
		f.env["CLAUDE_CONFIG_DIR"] = other
		f.env["CLAUDE_CODE_USE_BEDROCK"] = "1"
		f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", exp)

		got := Discover(f)

		h := func(dir string) string { return pathHash8(dir)[:4] }
		want := []string{
			"claude:proj/work@" + h(other),
			"claude:proj/work@" + h(filepath.Join(f.home, ".claude-work")),
		}
		slices.Sort(want)
		if diff := cmp.Diff(want, labelsOf(got)); diff != "" {
			t.Errorf("labels (-want +got):\n%s", diff)
		}
	})

	t.Run("a unique name keeps its short label", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", exp)
		f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", exp)
		if diff := cmp.Diff([]string{"claude", "claude:work"}, labelsOf(readClaude(f))); diff != "" {
			t.Errorf("labels (-want +got):\n%s", diff)
		}
	})

	t.Run("two unproven logins with one name both exist", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.env["CLAUDE_CONFIG_DIR"] = f.path("proj", ".claude-work")
		f.files[f.path("proj", ".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-1", "max", exp)
		f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-2", "max", exp)

		got := Discover(f)

		if len(got) != 2 || got[0].ID == got[1].ID {
			t.Errorf("got %d accounts with ids %q %q, want 2 distinct", len(got), got[0].ID, got[len(got)-1].ID)
		}
	})
}

func TestExpiredNoteNamesLabelAndDirectory(t *testing.T) {
	t.Parallel()
	past, later := testNow.Add(-time.Hour), testNow.Add(time.Hour)
	tests := []struct {
		name  string
		setup func(f *fakeStore)
		read  func(Store) []Account
		want  []string // notes of the expired rows
	}{
		{
			name: "default profile",
			setup: func(f *fakeStore) {
				f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", past)
			},
			read: readClaude,
			want: []string{"token expired, open claude to refresh"},
		},
		{
			name: "collapsed single non-default profile",
			setup: func(f *fakeStore) {
				f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", past)
			},
			read: readClaude,
			want: []string{"token expired, open claude (CLAUDE_CONFIG_DIR=~/.claude-work) to refresh"},
		},
		{
			name: "two profiles",
			setup: func(f *fakeStore) {
				f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", later)
				f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", past)
			},
			read: readClaude,
			want: []string{"token expired, open claude:work (CLAUDE_CONFIG_DIR=~/.claude-work) to refresh"},
		},
		{
			name: "env dir outside home keeps the full path",
			setup: func(f *fakeStore) {
				f.env["CLAUDE_CONFIG_DIR"] = filepath.FromSlash("/x/work")
				f.files[filepath.FromSlash("/x/work/.credentials.json")] = claudeCredsFor("fake-tok-w", "max", past)
			},
			read: readClaude,
			want: []string{"token expired, open claude (CLAUDE_CONFIG_DIR=" + shellQuote(filepath.FromSlash("/x/work")) + ") to refresh"},
		},
		{
			name: "codex",
			setup: func(f *fakeStore) {
				a, i := chatgptTokens(t, past, "acct-1")
				f.files[f.path(".codex-work", "auth.json")] = codexAuthJSON(t, a, i, "acct-1")
			},
			read: readCodex,
			want: []string{"token expired, open codex (CODEX_HOME=~/.codex-work) to refresh"},
		},
		{
			name: "pi",
			setup: func(f *fakeStore) {
				f.files[f.path(".pi", "agent-work", "auth.json")] = piCodexFile(identTokens(t, past, "acct-1", "user-1"), past, "acct-1")
			},
			read: readPi,
			want: []string{"token expired, open pi (PI_CODING_AGENT_DIR=~/.pi/agent-work) to refresh"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake("linux")
			tt.setup(f)
			var notes []string
			for _, a := range tt.read(f) {
				if a.Status != "" {
					notes = append(notes, a.Note)
				}
			}
			if diff := cmp.Diff(tt.want, notes); diff != "" {
				t.Errorf("notes (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSymlinkedProfileDirs(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	f := newFake("linux")
	root := string(filepath.Separator)
	f.canon = map[string]string{
		f.path(".claude-home"): f.home,
		f.path(".claude-root"): root,
		f.path(".claude-ext"):  filepath.FromSlash("/data/ext"),
	}
	for _, d := range []string{".claude-home", ".claude-root", ".claude-ext"} {
		f.files[f.path(d, ".credentials.json")] = claudeCredsFor("fake-tok-"+d, "max", exp)
	}
	f.files[f.path(".claude.json")] = claudeAccountJSON("u-1", "o-1")

	got := readClaude(f)

	if len(got) != 1 || got[0].Cred.Token() != "fake-tok-.claude-ext" {
		t.Errorf("got %v, want only the link to /data/ext", rows(got))
	}
}

func TestClaudeKeychainSuffixWhenConfigDirIsSet(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	f := newFake("darwin")
	dir := f.path(".claude")
	f.env["CLAUDE_CONFIG_DIR"] = dir
	f.secrets[claudeKeychainService+"\x00"] = claudeCredsFor("fake-tok-plain", "max", exp)
	f.secrets[claudeHashed(dir)+"\x00"] = claudeCredsFor("fake-tok-hashed", "max", exp)

	got := readClaude(f)

	if len(got) != 1 || got[0].Cred.Token() != "fake-tok-hashed" || got[0].Harnesses[0] != "claude" {
		t.Errorf("got %v, want the suffixed service of the default dir", rows(got))
	}
}

func TestListedProfilesIgnoreGlobMetacharactersInHome(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	f.home = filepath.FromSlash("/home/[us*r]")
	f.files[f.path(".claude", ".credentials.json")] = claudeCredsFor("fake-tok-d", "max", testNow.Add(time.Hour))
	f.files[f.path(".claude-work", ".credentials.json")] = claudeCredsFor("fake-tok-w", "max", testNow.Add(time.Hour))

	if diff := cmp.Diff([]string{"claude", "claude:work"}, labelsOf(readClaude(f))); diff != "" {
		t.Errorf("labels (-want +got):\n%s", diff)
	}
}

func TestCodexHomeIsTheProfileDirectory(t *testing.T) {
	t.Parallel()
	exp := testNow.Add(time.Hour)
	f := newFake("linux")
	f.env["CODEX_HOME"] = f.path(".codex-work")
	a1, i1 := chatgptTokens(t, exp, "acct-1")
	a2, i2 := chatgptTokens(t, exp, "acct-2")
	f.files[f.path(".codex", "auth.json")] = codexAuthJSON(t, a1, i1, "acct-1")
	f.files[f.path(".codex-work", "auth.json")] = codexAuthJSON(t, a2, i2, "acct-2")

	homes := map[string]string{}
	for _, a := range Discover(f) {
		homes[a.Harnesses[0]] = a.CodexHome
	}
	want := map[string]string{"codex": f.path(".codex"), "codex:work": f.path(".codex-work")}
	if diff := cmp.Diff(want, homes); diff != "" {
		t.Errorf("CodexHome by harness (-want +got):\n%s", diff)
	}

	t.Run("pi and other harness accounts have none", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.files[f.path(".pi", "agent", "auth.json")] = piCodexFile(a1, exp, "acct-1")
		for _, a := range Discover(f) {
			if a.CodexHome != "" {
				t.Errorf("pi account has CodexHome %q", a.CodexHome)
			}
		}
	})

	t.Run("merge keeps a non-empty home", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.files[f.path(".codex", "auth.json")] = codexAuthJSON(t, a1, i1, "acct-1")
		f.files[f.path(".pi", "agent", "auth.json")] = piCodexFile(a1, exp.Add(time.Hour), "acct-1")
		got := Discover(f)
		if len(got) != 1 || got[0].CodexHome != f.path(".codex") {
			t.Errorf("got %v home %q, want the codex dir kept", rows(got), got[0].CodexHome)
		}
	})
}
