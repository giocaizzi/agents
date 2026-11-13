package harness

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/mcpsync"
)

// RenderWorkspaces renders only the MCP files of the configured workspaces for
// the selected harnesses. Like Render, it returns the Plan with what rendered
// and an error joining the failures.
func RenderWorkspaces(r *config.Resolved, env Env) (Plan, error) {
	if env.ReadFile == nil {
		env.ReadFile = os.ReadFile
	}
	b := &builder{cfg: &r.Config, env: env, profile: r.Profile, seen: map[string]bool{}}
	if err := renderWorkspaces(b.cfg, b, Selected(r.Harnesses)); err != nil {
		b.errs = append(b.errs, fmt.Errorf("mcp workspaces: %w", err))
	}
	return Plan{Targets: b.targets, Warnings: b.warnings}, errors.Join(b.errs...)
}

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
		targets, warnings := WorkspaceTargets(w.Path, servers, names, false)
		for _, t := range targets {
			b.add(t)
		}
		for _, warning := range warnings {
			b.warnf("mcp.workspaces.%s: %s", w.Name, warning)
		}
	}
	return nil
}

// WorkspaceTargets returns the MCP files of the workspace at root for the
// named harnesses, and the warnings about servers a harness cannot take (only
// for the harnesses named). withoutSource leaves out .mcp.json, for a
// workspace whose source is that file.
func WorkspaceTargets(root string, servers map[string]mcpsync.ServerEntry, harnesses []string, withoutSource bool) ([]apply.Target, []string) {
	var targets []apply.Target
	for _, d := range mcpsync.Docs(servers, harnesses) {
		if withoutSource && d.RelPath == mcpsync.SourceFilename {
			continue
		}
		targets = append(targets, apply.Target{
			Owner:   d.Harness,
			Path:    filepath.Join(root, filepath.FromSlash(d.RelPath)),
			Format:  d.Format,
			Managed: d.Managed,
			Mode:    0o600, // a new file is private; git tracks no read bits
		})
	}
	var warnings []string
	for _, warning := range mcpsync.Warnings(servers) {
		if strings.HasPrefix(warning, "antigravity:") && !slices.Contains(harnesses, string(config.Antigravity)) {
			continue
		}
		if strings.HasPrefix(warning, "opencode ") && !slices.Contains(harnesses, string(config.OpenCode)) {
			continue
		}
		warnings = append(warnings, warning)
	}
	return targets, warnings
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
