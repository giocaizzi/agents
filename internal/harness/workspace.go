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
		for _, h := range names {
			servers := b.workspaceEntries("mcp.workspaces."+w.Name+".servers", h, w.Servers)
			targets, warnings := WorkspaceTargets(w.Path, servers, []string{h}, false)
			for _, t := range targets {
				b.add(t)
			}
			for _, warning := range warnings {
				b.warnf("mcp.workspaces.%s: %s", w.Name, warning)
			}
		}
	}
	return nil
}

// workspaceRef is how harness h reads an environment variable in a project
// file, as a format for the variable name; false when it reads none.
// Claude Code and Copilot expand ${VAR} in .mcp.json (VS Code reads the same
// file); opencode spells it {env:VAR}.
func workspaceRef(h string) (string, bool) {
	switch h {
	case string(config.Claude), string(config.Copilot), string(config.VSCode):
		return "${%s}", true
	case string(config.OpenCode):
		return "{env:%s}", true
	}
	return "", false
}

// workspaceEntries converts the servers of a workspace for harness h. A
// project file is committed, so a ${VAR} reference is never expanded into it:
// it stays a reference in the harness's own spelling, and a server with a
// reference is skipped (warned) for a harness that expands none.
func (b *builder) workspaceEntries(where, h string, in map[string]config.MCPServer) map[string]mcpsync.ServerEntry {
	style, expands := workspaceRef(h)
	out := make(map[string]mcpsync.ServerEntry, len(in))
	for _, name := range slices.Sorted(maps.Keys(in)) {
		var refs []string
		rewrite := func(v string) string {
			text, _ := config.Expand(v, func(n string) string {
				refs = append(refs, n)
				if expands {
					return fmt.Sprintf(style, n)
				}
				return n
			})
			return text
		}
		s := mapServer(in[name], rewrite)
		if len(refs) > 0 && !expands {
			slices.Sort(refs)
			b.warnf("%s.%s: skipped for %s, which does not expand ${%s} in a project file",
				where, name, h, strings.Join(slices.Compact(refs), "}, ${"))
			continue
		}
		out[name] = serverEntry(s)
	}
	return out
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
			Legacy:  workspaceLegacy(d),
			Mode:    0o600, // a new file is private; git tracks no read bits
		})
	}
	var warnings []string
	for _, warning := range mcpsync.Warnings(servers) {
		if strings.HasPrefix(warning, "antigravity:") && !slices.Contains(harnesses, string(config.Antigravity)) {
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

// workspaceLegacy are the flat mcp.<name> entries that earlier versions wrote
// into opencode.json, a layout opencode 2 deprecates in favour of mcp.servers.
func workspaceLegacy(d mcpsync.Doc) [][]string {
	if d.RelPath != mcpsync.OpenCodeRelPath {
		return nil
	}
	mcp, _ := d.Managed["mcp"].(map[string]any)
	servers, _ := mcp["servers"].(map[string]any)
	var out [][]string
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		out = append(out, []string{"mcp", name})
	}
	return out
}
