package mcpsync

import "github.com/giocaizzi/agents/internal/apply"

// Native files of a workspace, relative to the workspace root, besides the
// ones that have their own constant.
const (
	// OpenCodeRelPath is opencode's project config.
	OpenCodeRelPath = "opencode.json"

	openCodeSchema = "https://opencode.ai/config.json"
)

// Doc is the managed part of one native MCP file of a workspace: the keys
// agents owns, to be merged into the live file by the apply engine. Managed
// holds only map[string]any, []any, strings, ints and bools, so the engine can
// render it as JSON or TOML. It carries no generated-by marker: the apply
// manifest records ownership.
type Doc struct {
	Harness string // the harness the file belongs to; a shared file names the first one asked for
	RelPath string // path relative to the workspace root
	Format  apply.Format
	Managed map[string]any
}

// Docs returns the managed document of every native file the named harnesses
// read for the server set, in the order of harnesses:
//
//   - claude, copilot, vscode: .mcp.json (shared, so repeated once per harness
//     asked for) with Claude's nested oauth object and Copilot's flat
//     oauthClientId, oauthPublicClient and auth keys, as in the source;
//   - codex: .codex/config.toml (the mcp_servers table);
//   - pi: .pi/mcp.json;
//   - opencode: opencode.json (servers with an OAuth client are left out:
//     agents has no verified translation of it);
//   - antigravity: .agents/mcp_config.json (no callbackPort, which
//     Antigravity cannot set).
//
// Unknown harness names are ignored.
func Docs(servers map[string]ServerEntry, harnesses []string) []Doc {
	var docs []Doc
	for _, h := range harnesses {
		switch h {
		case "claude", "copilot", "vscode":
			docs = append(docs, Doc{h, SourceFilename, apply.JSON, sourceDoc(servers)})
		case "codex":
			docs = append(docs, Doc{h, CodexTargetRelPath, apply.TOML, codexDoc(servers)})
		case "pi":
			docs = append(docs, Doc{h, PiTargetRelPath, apply.JSON, nestedDoc(servers, true)})
		case "opencode":
			docs = append(docs, Doc{h, OpenCodeRelPath, apply.JSON, openCodeDoc(servers)})
		case "antigravity":
			docs = append(docs, Doc{h, AntigravityRelPath, apply.JSON, nestedDoc(servers, false)})
		}
	}
	return docs
}

// common returns the keys every JSON style shares.
func common(e ServerEntry) map[string]any {
	s := map[string]any{}
	setString(s, "type", e.Type)
	setString(s, "url", e.URL)
	setString(s, "command", e.Command)
	if len(e.Args) > 0 {
		s["args"] = anySlice(e.Args)
	}
	setStrings(s, "headers", e.Headers)
	setStrings(s, "env", e.Env)
	return s
}

// sourceDoc keeps both OAuth key styles verbatim.
func sourceDoc(servers map[string]ServerEntry) map[string]any {
	out := map[string]any{}
	for name, e := range servers {
		s := common(e)
		if e.OAuth != nil {
			s["oauth"] = oauthMap(e.OAuth.ClientID, e.OAuth.CallbackPort)
		}
		setString(s, "oauthClientId", e.OAuthClientID)
		if e.OAuthPublicClient != nil {
			s["oauthPublicClient"] = *e.OAuthPublicClient
		}
		if e.Auth != nil {
			s["auth"] = map[string]any{"redirectPort": e.Auth.RedirectPort}
		}
		out[name] = s
	}
	return map[string]any{"mcpServers": out}
}

// nestedDoc is the pi and Antigravity shape: Claude's nested oauth only.
func nestedDoc(servers map[string]ServerEntry, withPort bool) map[string]any {
	out := map[string]any{}
	for name, e := range servers {
		s := common(e)
		clientID, port := e.oauthClient()
		if !withPort {
			port = 0
		}
		if clientID != "" || port != 0 {
			s["oauth"] = oauthMap(clientID, port)
		}
		out[name] = s
	}
	return map[string]any{"mcpServers": out}
}

func codexDoc(servers map[string]ServerEntry) map[string]any {
	out := map[string]any{}
	for name, e := range servers {
		s := map[string]any{}
		switch {
		case e.URL != "":
			s["url"] = e.URL
			setStrings(s, "http_headers", e.Headers)
			clientID, port := e.oauthClient()
			if clientID != "" || port != 0 {
				o := map[string]any{}
				setString(o, "client_id", clientID)
				if port != 0 {
					o["callback_port"] = port
				}
				s["oauth"] = o
			}
		case e.Command != "":
			s["command"] = e.Command
			if len(e.Args) > 0 {
				s["args"] = anySlice(e.Args)
			}
			setStrings(s, "env", e.Env)
		default:
			continue
		}
		out[name] = s
	}
	return map[string]any{"mcp_servers": out}
}

func openCodeDoc(servers map[string]ServerEntry) map[string]any {
	out := map[string]any{}
	for name, e := range servers {
		s := map[string]any{}
		switch {
		case e.URL != "":
			s["type"] = "remote"
			s["url"] = e.URL
			setStrings(s, "headers", e.Headers)
			clientID, port := e.oauthClient()
			if clientID != "" || port != 0 {
				o := map[string]any{}
				setString(o, "client_id", clientID)
				if port != 0 {
					o["callback_port"] = port
				}
				s["oauth"] = o
			}
		case e.Command != "":
			s["type"] = "local"
			s["command"] = anySlice(append([]string{e.Command}, e.Args...))
			setStrings(s, "environment", e.Env)
		default:
			continue
		}
		out[name] = s
	}
	return map[string]any{"$schema": openCodeSchema, "mcp": map[string]any{"servers": out}}
}

func oauthMap(clientID string, port int) map[string]any {
	o := map[string]any{}
	setString(o, "clientId", clientID)
	if port != 0 {
		o["callbackPort"] = port
	}
	return o
}

func setString(m map[string]any, key, v string) {
	if v != "" {
		m[key] = v
	}
}

func setStrings(m map[string]any, key string, v map[string]string) {
	if len(v) == 0 {
		return
	}
	out := make(map[string]any, len(v))
	for k, s := range v {
		out[k] = s
	}
	m[key] = out
}

func anySlice(v []string) []any {
	out := make([]any, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}
