package provider

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/giocaizzi/agents/internal/usage"
)

const (
	copilotKeychainService = "copilot-cli"
	defaultGitHubHost      = "https://github.com"
)

type copilotUser struct{ login, host string }

// normalizeHost returns a GitHub host as a lowercase https URL without a
// trailing slash; a bare domain gets the https:// prefix. An empty value is
// the default host. Nothing else is interpreted: an odd value stays odd, so
// the caller does not treat it as github.com.
func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.TrimRight(h, "/")
	if h == "" {
		return defaultGitHubHost
	}
	if !strings.Contains(h, "://") {
		h = "https://" + h
	}
	return h
}

// parseCopilotUser accepts a bare login string or a {login, host} object.
func parseCopilotUser(raw json.RawMessage) (copilotUser, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return copilotUser{login: s, host: defaultGitHubHost}, true
	}
	var o struct {
		Login string `json:"login"`
		Host  string `json:"host"`
	}
	if json.Unmarshal(raw, &o) == nil && o.Login != "" {
		if o.Host == "" {
			o.Host = defaultGitHubHost
		}
		return copilotUser{login: o.Login, host: o.Host}, true
	}
	return copilotUser{}, false
}

// copilotLogins lists the logged-in users and the active login: the
// lastLoggedInUser, else the first.
func copilotLogins(users []json.RawMessage, last json.RawMessage) (logins []copilotUser, active string) {
	seen := map[string]bool{}
	add := func(u copilotUser) {
		if !seen[u.login] {
			seen[u.login] = true
			logins = append(logins, u)
		}
	}
	for _, r := range users {
		if u, ok := parseCopilotUser(r); ok {
			add(u)
		}
	}
	if u, ok := parseCopilotUser(last); ok {
		add(u)
		return logins, u.login
	}
	if len(logins) > 0 {
		active = logins[0].login
	}
	return logins, active
}

// readCopilot lists the logins in config.json (JSONC) and looks up each token.
// A token from the environment is attributed to the active login only, since
// it cannot belong to several accounts. The docs-described plaintext token
// fallback is not read.
func readCopilot(st Store) []Account {
	dir := st.Getenv("COPILOT_HOME")
	if dir == "" {
		dir = filepath.Join(st.Home(), ".copilot")
	}
	b, err := st.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		if isNotExist(err) {
			return nil
		}
		return []Account{unreadable(GitHubCopilot, "copilot")}
	}
	var cfg struct {
		LoggedInUsers    []json.RawMessage `json:"loggedInUsers"`
		LastLoggedInUser json.RawMessage   `json:"lastLoggedInUser"`
	}
	if err := unmarshalQuiet(stripJSONC(b), &cfg); err != nil {
		return []Account{unreadable(GitHubCopilot, "copilot")}
	}
	users, active := copilotLogins(cfg.LoggedInUsers, cfg.LastLoggedInUser)

	envToken := ""
	for _, k := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if envToken = st.Getenv(k); envToken != "" {
			break
		}
	}

	out := make([]Account, 0, len(users))
	for _, u := range users {
		a := Account{
			Provider:  GitHubCopilot,
			ID:        "copilot:" + normalizeHost(u.host) + ":" + u.login,
			Label:     u.login,
			Login:     u.login,
			Host:      normalizeHost(u.host), // the keychain lookup keeps the host as written
			Harnesses: []string{"copilot"},
			Auth:      AuthOAuth,
			Plan:      unknownPlan(),
		}
		proven(&a, CopilotKey(u.host, u.login))
		token := ""
		if envToken != "" && u.login == active {
			token, a.Source = envToken, srcEnv
		} else if s, err := st.Secret(copilotKeychainService, u.host+":"+u.login+":github"); err == nil {
			token, a.Source = s, srcKeychain
		}
		if token == "" {
			a.Status = usage.StatusUnavailable
			a.Note = "token not found in the credential store"
		} else {
			a.Cred = NewCredential(token, "")
		}
		out = append(out, a)
	}
	return out
}
