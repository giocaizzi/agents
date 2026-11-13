package harness

import (
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/mcpsync"
)

// renderWorkspaces adds the MCP files of every workspace for the selected harnesses.
//
//nolint:unparam // the error result keeps the signature of the other renderers
func renderWorkspaces(_ *config.Config, b *builder, selected []config.Harness) error {
	names := make([]string, len(selected))
	for i, h := range selected {
		names[i] = string(h)
	}
	for _, w := range b.workspaces() {
		servers := make(map[string]mcpsync.ServerEntry, len(w.Servers))
		for name, s := range w.Servers {
			servers[name] = serverEntry(s)
		}
		for _, d := range mcpsync.Docs(servers, names) {
			b.add(apply.Target{
				Owner:   d.Harness,
				Path:    filepath.Join(w.Path, filepath.FromSlash(d.RelPath)),
				Format:  d.Format,
				Managed: d.Managed,
				Mode:    workspaceMode(d.RelPath),
			})
		}
		for _, warning := range mcpsync.Warnings(servers) {
			if strings.HasPrefix(warning, "antigravity:") && !slices.Contains(names, string(config.Antigravity)) {
				continue
			}
			b.warnf("mcp.workspaces.%s: %s", w.Name, warning)
		}
	}
	return nil
}

// workspaceMode is the mode of a new workspace file. .mcp.json and
// opencode.json are shared project files that people commit and read, so they
// stay world-readable (0o644); the per-harness files are private to the user
// (0o600), like the files agents writes in the home directory.
func workspaceMode(rel string) fs.FileMode {
	if rel == mcpsync.SourceFilename || rel == mcpsync.OpenCodeRelPath {
		return 0o644
	}
	return 0o600
}

// serverEntry converts a configured server to the mcpsync shape. The type is
// inferred from url or command; an OAuth client is written in both key styles
// (Claude's nested object, Copilot's flat keys) with oauthPublicClient false
// unless the config says it is public.
func serverEntry(s config.MCPServer) mcpsync.ServerEntry {
	e := mcpsync.ServerEntry{
		Type: s.Type, URL: s.URL, Command: s.Command,
		Headers: maps.Clone(s.Headers), Args: slices.Clone(s.Args), Env: maps.Clone(s.Env),
	}
	if e.Type == "" {
		switch {
		case s.URL != "":
			e.Type = mcpsync.TransportHTTP
		case s.Command != "":
			e.Type = mcpsync.TransportStdio
		}
	}
	o := s.OAuth
	if o.ClientID == "" && o.CallbackPort == 0 && o.PublicClient == nil {
		return e
	}
	e.OAuth = &mcpsync.OAuth{ClientID: o.ClientID, CallbackPort: o.CallbackPort}
	if o.CallbackPort != 0 {
		e.Auth = &mcpsync.Auth{RedirectPort: o.CallbackPort}
	}
	if o.ClientID != "" {
		e.OAuthClientID = o.ClientID
		public := o.PublicClient != nil && *o.PublicClient
		e.OAuthPublicClient = &public
	}
	return e
}
