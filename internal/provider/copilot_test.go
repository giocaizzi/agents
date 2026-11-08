package provider

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestStripJSONC(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("testdata/copilot-config.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Banner string `json:"banner"`
		Esc    string `json:"esc"`
	}
	if err := json.Unmarshal(stripJSONC(src), &got); err != nil {
		t.Fatalf("stripped JSONC does not parse: %v", err)
	}
	want := struct {
		Banner string `json:"banner"`
		Esc    string `json:"esc"`
	}{"see https://example.com/a//b and /* not a comment */", `quote " // still a string`}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestCopilot(t *testing.T) {
	t.Parallel()
	cfg, err := os.ReadFile("testdata/copilot-config.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	setup := func() *fakeStore {
		f := newFake("darwin")
		f.files[f.path(".copilot", "config.json")] = string(cfg)
		return f
	}

	t.Run("keychain per login", func(t *testing.T) {
		t.Parallel()
		f := setup()
		f.secrets["copilot-cli\x00https://github.com:octocat:github"] = "fake-token-1"
		f.secrets["copilot-cli\x00https://github.com:hubot:github"] = "fake-token-2"
		assertViews(t, readCopilot(f), []view{
			{Provider: GitHubCopilot, Label: "octocat", Key: "copilot:https://github.com:octocat", Login: "octocat", Harnesses: []string{"copilot"}, Auth: AuthOAuth, Token: "fake-token-1", Source: "keychain"},
			{Provider: GitHubCopilot, Label: "hubot", Key: "copilot:https://github.com:hubot", Login: "hubot", Harnesses: []string{"copilot"}, Auth: AuthOAuth, Token: "fake-token-2", Source: "keychain"},
		})
	})
	t.Run("env token goes to the active login only", func(t *testing.T) {
		t.Parallel()
		f := setup()
		f.env["GH_TOKEN"] = "fake-token-1"
		f.env["GITHUB_TOKEN"] = "fake-token-3"
		got := readCopilot(f)
		assertViews(t, got, []view{
			{Provider: GitHubCopilot, Label: "octocat", Key: "copilot:https://github.com:octocat", Login: "octocat", Harnesses: []string{"copilot"}, Auth: AuthOAuth, Token: "fake-token-1", Source: "env"},
			{Provider: GitHubCopilot, Label: "hubot", Key: "copilot:https://github.com:hubot", Login: "hubot", Harnesses: []string{"copilot"}, Auth: AuthOAuth, Status: "unavailable", Note: "token not found in the credential store"},
		})
	})
	t.Run("COPILOT_GITHUB_TOKEN wins", func(t *testing.T) {
		t.Parallel()
		f := setup()
		f.env["COPILOT_GITHUB_TOKEN"] = "fake-token-9"
		f.env["GH_TOKEN"] = "fake-token-1"
		if got := readCopilot(f); got[0].Cred.Token() != "fake-token-9" {
			t.Errorf("env precedence broken")
		}
	})
	t.Run("COPILOT_HOME", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.env["COPILOT_HOME"] = "/cp"
		f.files["/cp/config.json"] = `{"loggedInUsers":["octocat"]}`
		got := readCopilot(f)
		if len(got) != 1 || got[0].Login != "octocat" {
			t.Errorf("got %+v", views(got))
		}
	})
	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		assertViews(t, readCopilot(newFake("linux")), nil)
	})
	t.Run("malformed", func(t *testing.T) {
		t.Parallel()
		f := newFake("linux")
		f.files[f.path(".copilot", "config.json")] = `{ nope`
		assertViews(t, readCopilot(f), []view{{Provider: GitHubCopilot, Harnesses: []string{"copilot"}, Auth: AuthNone, Status: "unavailable", Note: "credentials unreadable"}})
	})
}

func TestCopilotHost(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ cfg, want string }{
		"default":         {`{"loggedInUsers":["octocat"]}`, "https://github.com"},
		"object default":  {`{"loggedInUsers":[{"login":"octocat"}]}`, "https://github.com"},
		"enterprise":      {`{"loggedInUsers":[{"login":"octocat","host":"https://acme.ghe.com"}]}`, "https://acme.ghe.com"},
		"normalized":      {`{"loggedInUsers":[{"login":"octocat","host":"ACME.ghe.com/"}]}`, "https://acme.ghe.com"},
		"last logged in":  {`{"loggedInUsers":[],"lastLoggedInUser":{"login":"octocat","host":"https://ghe.example.com"}}`, "https://ghe.example.com"},
		"unparsable host": {`{"loggedInUsers":[{"login":"octocat","host":"weird host"}]}`, "https://weird host"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFake("linux")
			f.files[f.path(".copilot", "config.json")] = tc.cfg
			got := readCopilot(f)
			if len(got) != 1 || got[0].Host != tc.want {
				t.Errorf("got %+v, want host %q", views(got), tc.want)
			}
		})
	}
}

func TestAntigravity(t *testing.T) {
	t.Parallel()
	f := newFake("linux")
	assertViews(t, readAntigravity(f), nil)

	f.files[f.path(".gemini", "antigravity-cli", "settings.json")] = "{}"
	got := readAntigravity(f)
	assertViews(t, got, []view{{Provider: Google, Key: "antigravity", Harnesses: []string{"antigravity"}, Auth: AuthCLI, Source: "file"}})
	if got[0].Plan.Raw != "antigravity" || got[0].Plan.Name != "Antigravity" || !got[0].Cred.Empty() {
		t.Errorf("Plan = %+v", got[0].Plan)
	}
}
