// Package harness renders a resolved configuration into the native files of
// every selected harness, as apply targets: the keys agents manages in each
// file, merged into the live file by the apply engine.
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
	"github.com/giocaizzi/agents/internal/instructions"
)

// Env is everything Render reads from the machine. Every field is required
// except ReadFile (default os.ReadFile).
type Env struct {
	Home     string                       // user home directory
	Hostname string                       // host name, for telemetry and instructions
	Arch     string                       // GOARCH, for telemetry
	GOOS     string                       // target operating system
	Getenv   func(string) string          // environment, for ${VAR} expansion
	LookPath func(string) (string, error) // finds a language server on PATH
	ReadFile func(string) ([]byte, error) // reads instruction layers
	Dirs     config.Dirs                  // agents' own directories
	Agents   []string                     // installed agent ids (plugin:agent), to expand glob assignments; may be empty
}

// Plan is the rendered result: the targets to apply, in a stable order, and
// the warnings (skipped entries) to show the user.
type Plan struct {
	Targets  []apply.Target
	Warnings []string
}

// Render renders the targets of every selected harness and of every MCP
// workspace. The error joins every failure (instruction layers, invalid
// values); the Plan is still returned with what rendered.
func Render(r *config.Resolved, env Env) (Plan, error) {
	if env.Getenv == nil || env.LookPath == nil {
		return Plan{}, errors.New("harness: Env.Getenv and Env.LookPath are required")
	}
	if env.ReadFile == nil {
		env.ReadFile = os.ReadFile
	}
	b := &builder{cfg: &r.Config, env: env, profile: r.Profile, seen: map[string]bool{}}
	selected := Selected(r.Harnesses)
	for _, h := range selected {
		if err := renderers[h](b.cfg, b); err != nil {
			b.errs = append(b.errs, fmt.Errorf("%s: %w", h, err))
		}
	}
	if err := renderWorkspaces(b.cfg, b, selected); err != nil {
		b.errs = append(b.errs, fmt.Errorf("mcp workspaces: %w", err))
	}
	return Plan{Targets: b.targets, Warnings: b.warnings}, errors.Join(b.errs...)
}

// Selected returns the selected harnesses in canonical order, without
// duplicates or unknown names.
func Selected(hs []config.Harness) []config.Harness {
	var out []config.Harness
	for _, h := range config.All() {
		if slices.Contains(hs, h) {
			out = append(out, h)
		}
	}
	return out
}

// renderer adds the targets of one harness to b.
type renderer func(c *config.Config, b *builder) error

//nolint:gochecknoglobals // immutable dispatch table
var renderers = map[config.Harness]renderer{
	config.Claude:      renderClaude,
	config.Copilot:     renderCopilot,
	config.VSCode:      renderVSCode,
	config.Codex:       renderCodex,
	config.OpenCode:    renderOpenCode,
	config.Pi:          renderPi,
	config.Antigravity: renderAntigravity,
}

// builder collects targets and warnings while the renderers run.
type builder struct {
	cfg      *config.Config
	env      Env
	profile  string
	targets  []apply.Target
	warnings []string
	errs     []error
	seen     map[string]bool // target paths already added
	expanded map[string]bool // values substituted for ${VAR} so far

	lspDone bool
	lsp     []LSPServer
	mcpDone bool
	mcp     map[string]config.MCPServer
	wsDone  bool
	ws      []Workspace
}

// add appends a target. A second target for the same path is dropped: the
// first harness in canonical order owns a shared file (copilot before vscode
// for ~/.copilot).
func (b *builder) add(t apply.Target) {
	if b.seen[t.Path] {
		return
	}
	b.seen[t.Path] = true
	t.Sensitive = b.sensitiveIn(t.Managed)
	b.targets = append(b.targets, t)
}

// getenv reads a variable for ${VAR} expansion and remembers its value, so a
// target that ends up holding it can be redacted in terminal output.
func (b *builder) getenv(name string) string {
	v := b.env.Getenv(name)
	if v != "" {
		if b.expanded == nil {
			b.expanded = map[string]bool{}
		}
		b.expanded[v] = true
	}
	return v
}

// sensitiveIn lists, sorted, the expanded values that managed content holds.
func (b *builder) sensitiveIn(managed any) []string {
	var out []string
	text := fmt.Sprint(managed)
	for v := range b.expanded {
		if strings.Contains(text, v) {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

// warnf records a warning, once per distinct text.
func (b *builder) warnf(format string, args ...any) {
	w := fmt.Sprintf(format, args...)
	if !slices.Contains(b.warnings, w) {
		b.warnings = append(b.warnings, w)
	}
}

// home joins elem onto the home directory.
func (b *builder) home(elem ...string) string {
	return filepath.Join(append([]string{b.env.Home}, elem...)...)
}

// expand expands ${VAR} references in s. When one is unset it warns, naming
// where, and reports false: the caller skips the entry.
func (b *builder) expand(where, s string) (string, bool) {
	out, unset := config.Expand(s, b.getenv)
	if len(unset) > 0 {
		b.warnf("%s: skipped, ${%s} is unset", where, strings.Join(unset, "}, ${"))
		return "", false
	}
	return out, true
}

// expandPath is expand with a leading ~ replaced by the home directory.
func (b *builder) expandPath(where, p string) (string, bool) {
	out, unset := config.ExpandPath(p, b.env.Home, b.getenv)
	if len(unset) > 0 {
		b.warnf("%s: skipped, ${%s} is unset", where, strings.Join(unset, "}, ${"))
		return "", false
	}
	return out, true
}

// PluginStates returns the plugins harness h knows about: every plugin
// enabled for h, true unless plugins.disabled lists it (then false).
func PluginStates(c *config.Config, h config.Harness) map[string]bool {
	out := map[string]bool{}
	for name, hs := range c.Plugins.Enabled {
		if slices.Contains(hs, h) {
			out[name] = !slices.Contains(c.Plugins.Disabled, name)
		}
	}
	return out
}

// MarketplaceName is the marketplace name of the plugin registry
// ("owner/repo" becomes "owner-repo"), "" without a registry.
func MarketplaceName(c *config.Config) string {
	return strings.ReplaceAll(c.Plugins.Registry, "/", "-")
}

// RegistryDir is the local checkout of the plugin registry:
// <data>/registries/<owner>/<repo>, "" without a registry.
func RegistryDir(c *config.Config, dirs config.Dirs) string {
	if c.Plugins.Registry == "" {
		return ""
	}
	return filepath.Join(dirs.Data, "registries", filepath.FromSlash(c.Plugins.Registry))
}

// Telemetry is the resolved OpenTelemetry export for one harness.
type Telemetry struct {
	Endpoint       string // expanded collector endpoint
	Protocol       string
	Temporality    string
	LogToolDetails bool
	LogToolContent bool
	Namespace      string            // telemetry.service_namespace
	Service        string            // telemetry.services[<harness>], else the harness name
	Attributes     map[string]string // telemetry.attributes
	Hostname       string
	Arch           string
}

// telemetry resolves the export for h; false when telemetry is off or its
// endpoint has an unset variable (warned).
func (b *builder) telemetry(h config.Harness) (Telemetry, bool) {
	t := b.cfg.Telemetry
	if !t.Exports(h) || t.Endpoint == "" {
		return Telemetry{}, false
	}
	endpoint, ok := b.expand("telemetry.endpoint", t.Endpoint)
	if !ok {
		return Telemetry{}, false
	}
	service := t.Services[string(h)]
	if service == "" {
		service = string(h)
	}
	return Telemetry{
		Endpoint: endpoint, Protocol: t.Protocol, Temporality: t.Temporality,
		LogToolDetails: t.LogToolDetails, LogToolContent: t.LogToolContent,
		Namespace: t.ServiceNamespace, Service: service, Attributes: t.Attributes,
		Hostname: b.env.Hostname, Arch: b.env.Arch,
	}, true
}

// LSPServer is one language server available on this machine.
type LSPServer struct {
	Name       string
	Command    []string // argv; a Windows .cmd or .bat shim runs through cmd /c
	Extensions map[string]string
	Filetypes  []string // the configured ones, else the sorted distinct language ids
	Format     bool
}

// lspServers lists the language servers whose command is on PATH, sorted by
// name; nil when lsp is off. Each skipped server is warned once.
func (b *builder) lspServers() []LSPServer {
	if b.lspDone {
		return b.lsp
	}
	b.lspDone = true
	if !b.cfg.LSP.Enabled {
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(b.cfg.LSP.Servers)) {
		s := b.cfg.LSP.Servers[name]
		if len(s.Command) == 0 {
			continue
		}
		found, err := b.env.LookPath(s.Command[0])
		if err != nil {
			b.warnf("lsp.servers.%s: skipped, %s is not on PATH", name, s.Command[0])
			continue
		}
		cmd := slices.Clone(s.Command)
		ext := strings.ToLower(filepath.Ext(found))
		if b.env.GOOS == goosWindows && (ext == ".cmd" || ext == ".bat") {
			cmd = append([]string{"cmd", "/c"}, cmd...)
		}
		ft := slices.Clone(s.Filetypes)
		if len(ft) == 0 {
			ft = slices.Compact(slices.Sorted(maps.Values(s.Extensions)))
		}
		b.lsp = append(b.lsp, LSPServer{Name: name, Command: cmd, Extensions: s.Extensions, Filetypes: ft, Format: s.Format})
	}
	return b.lsp
}

// mcpServers returns the user MCP servers with ${VAR} expanded in every
// string field; a server with an unset variable is skipped (warned once).
func (b *builder) mcpServers() map[string]config.MCPServer {
	if !b.mcpDone {
		b.mcpDone = true
		b.mcp = b.expandServers("mcp.servers", b.cfg.MCP.Servers)
	}
	return b.mcp
}

// Workspace is one MCP workspace with its path expanded. Its servers keep
// their ${VAR} references, which land in committed project files: the
// renderer rewrites or refuses them per harness. Only the OAuth client id, a
// public value, is expanded.
type Workspace struct {
	Name    string
	Path    string
	Servers map[string]config.MCPServer
}

// workspaces returns the MCP workspaces sorted by name, expanded; a workspace
// whose path has an unset variable is skipped (warned once).
func (b *builder) workspaces() []Workspace {
	if b.wsDone {
		return b.ws
	}
	b.wsDone = true
	for _, name := range slices.Sorted(maps.Keys(b.cfg.MCP.Workspaces)) {
		w := b.cfg.MCP.Workspaces[name]
		where := "mcp.workspaces." + name
		path, ok := b.expandPath(where+".path", w.Path)
		if !ok {
			continue
		}
		b.ws = append(b.ws, Workspace{Name: name, Path: path, Servers: b.workspaceServers(where+".servers", w.Servers)})
	}
	return b.ws
}

func (b *builder) expandServers(where string, in map[string]config.MCPServer) map[string]config.MCPServer {
	out := map[string]config.MCPServer{}
	for _, name := range slices.Sorted(maps.Keys(in)) {
		s, ok := b.expandServer(where+"."+name, in[name])
		if ok {
			out[name] = s
		}
	}
	return out
}

// workspaceServers keeps the ${VAR} references of the servers and expands
// only the OAuth client id; a server whose client id has an unset variable is
// skipped (warned).
func (b *builder) workspaceServers(where string, in map[string]config.MCPServer) map[string]config.MCPServer {
	out := map[string]config.MCPServer{}
	for _, name := range slices.Sorted(maps.Keys(in)) {
		s := in[name]
		id, ok := b.expand(where+"."+name, s.OAuth.ClientID)
		if !ok {
			continue
		}
		s.OAuth.ClientID = id
		out[name] = s
	}
	return out
}

func (b *builder) expandServer(where string, s config.MCPServer) (config.MCPServer, bool) {
	var missing []string
	ex := func(v string) string {
		out, unset := config.Expand(v, b.getenv)
		missing = append(missing, unset...)
		return out
	}
	s = mapServer(s, ex)
	s.OAuth.ClientID = ex(s.OAuth.ClientID)
	if len(missing) > 0 {
		slices.Sort(missing)
		b.warnf("%s: skipped, ${%s} is unset", where, strings.Join(slices.Compact(missing), "}, ${"))
		return config.MCPServer{}, false
	}
	return s, true
}

// mapServer applies f to every string of s except the OAuth client id.
func mapServer(s config.MCPServer, f func(string) string) config.MCPServer {
	mapStrings := func(m map[string]string) map[string]string {
		if m == nil {
			return nil
		}
		out := make(map[string]string, len(m))
		for k, v := range m {
			out[k] = f(v)
		}
		return out
	}
	s.URL = f(s.URL)
	s.Command = f(s.Command)
	s.Headers = mapStrings(s.Headers)
	s.Env = mapStrings(s.Env)
	if s.Args != nil {
		args := make([]string, len(s.Args))
		for i, a := range s.Args {
			args[i] = f(a)
		}
		s.Args = args
	}
	return s
}

// InstructionSpec builds the instruction composition of c. dir is the
// expanded instructions directory.
func InstructionSpec(c *config.Config, dir string) instructions.Spec {
	s := instructions.Spec{
		Dir:      dir,
		Layers:   map[string]instructions.Layer{},
		Vars:     c.Instructions.Vars,
		Profiles: map[string][]string{},
		Harness:  map[string][]string{},
		Cells:    map[string]map[string][]string{},
	}
	for name, l := range c.Instructions.Layers {
		s.Layers[name] = instructions.Layer{File: l.File}
	}
	for h, list := range c.Harness.InstructionLists() {
		if len(list) > 0 {
			s.Harness[string(h)] = list
		}
	}
	for p, prof := range c.Profiles {
		s.Profiles[p] = prof.Instructions
		cells := map[string][]string{}
		for h, list := range prof.Harness.InstructionLists() {
			if len(list) > 0 {
				cells[string(h)] = list
			}
		}
		s.Cells[p] = cells
	}
	return s
}

// instructions adds the composed instructions of harness h as a text target
// at path. An empty composition adds nothing; a layer problem is an error.
func (b *builder) instructions(h config.Harness, path string) {
	dir, ok := b.expandPath("instructions.dir", b.cfg.Instructions.Dir)
	if !ok {
		return
	}
	spec := InstructionSpec(b.cfg, dir)
	ctx := instructions.Context{Profile: b.profile, Harness: string(h), OS: b.env.GOOS, Hostname: b.env.Hostname}
	text, err := spec.Render(ctx, b.env.ReadFile)
	if err != nil {
		b.errs = append(b.errs, fmt.Errorf("%s instructions: %w", h, err))
		return
	}
	if text == "" {
		return
	}
	b.add(apply.Target{Owner: string(h), Path: path, Format: apply.Text, Managed: text, Mode: 0o600})
}
