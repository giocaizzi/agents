package config

import (
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/giocaizzi/agents/internal/finding"
)

// Validate checks a loaded Config for what the schema cannot say. Profiles are
// overlays, so their MCP servers and language servers may be partial; only the
// merged config and the workspaces are held to the complete-entry rules.
func Validate(c Config) []finding.Finding {
	var out []finding.Finding
	add := func(where, format string, args ...any) {
		out = append(out, finding.New(where, format, args...))
	}

	if c.SchemaVersion != 1 {
		add("schema_version", "must be 1, got %d", c.SchemaVersion)
	}
	for i, h := range c.Harnesses {
		if !h.Valid() {
			add(fmt.Sprintf("harnesses[%d]", i), "unknown harness %q (want one of %s)", h, harnessList())
		}
	}
	for i, h := range c.Telemetry.Harnesses {
		if !h.Valid() {
			add(fmt.Sprintf("telemetry.harnesses[%d]", i), "unknown harness %q (want one of %s)", h, harnessList())
		}
	}
	validatePlugins("plugins", c.Plugins, add)
	if c.Profile != "" {
		if _, ok := c.Profiles[c.Profile]; !ok {
			add("profile", "profile %q is not declared under profiles", c.Profile)
		}
	}

	validateLayerRefs(c, add)
	validateModels(c.Models, add)
	validateReasons(c.Permissions, add)
	validateAutonomy("autonomy", c.Autonomy, add)
	for _, o := range []struct {
		name string
		a    Autonomy
	}{{"os.darwin", c.OS.Darwin.Autonomy}, {"os.linux", c.OS.Linux.Autonomy}, {"os.windows", c.OS.Windows.Autonomy}} {
		validateAutonomy(o.name+".autonomy", o.a, add)
	}
	for _, name := range slices.Sorted(maps.Keys(c.Profiles)) {
		p := c.Profiles[name]
		base := "profiles." + name
		validatePlugins(base+".plugins", p.Plugins, add)
		validateAutonomy(base+".autonomy", p.Autonomy, add)
		validateServers(base+".mcp.servers", p.MCP.Servers, false, add)
	}

	for _, name := range slices.Sorted(maps.Keys(c.LSP.Servers)) {
		if len(c.LSP.Servers[name].Command) == 0 {
			add("lsp.servers."+name+".command", "must not be empty")
		}
	}
	validateServers("mcp.servers", c.MCP.Servers, true, add)
	for _, name := range slices.Sorted(maps.Keys(c.MCP.Workspaces)) {
		w := c.MCP.Workspaces[name]
		where := "mcp.workspaces." + name
		if strings.TrimSpace(w.Path) == "" {
			add(where+".path", "must not be empty")
		}
		validateServers(where+".servers", w.Servers, true, add)
	}
	return out
}

// validateLayerRefs checks that every instructions list names declared layers.
func validateLayerRefs(c Config, add func(string, string, ...any)) {
	layers := c.Instructions.Layers
	checkLayers := func(where string, names []string) {
		for _, n := range names {
			if _, ok := layers[n]; !ok {
				add(where, "layer %q is not declared under instructions.layers", n)
			}
		}
	}
	for _, h := range All() {
		checkLayers("harness."+string(h)+".instructions", c.Harness.InstructionLists()[h])
	}
	for _, name := range slices.Sorted(maps.Keys(c.Profiles)) {
		p := c.Profiles[name]
		base := "profiles." + name
		checkLayers(base+".instructions", p.Instructions)
		for _, h := range All() {
			checkLayers(base+".harness."+string(h)+".instructions", p.Harness.InstructionLists()[h])
		}
	}
}

// validateModels checks that the default tier and every assignment name a
// declared tier, and that per-harness efforts name known harnesses.
func validateModels(m Models, add func(string, string, ...any)) {
	if m.Default != "" {
		if _, ok := m.Tiers[m.Default]; !ok {
			add("models.default", "tier %q is not declared under models.tiers", m.Default)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(m.Assign)) {
		if _, ok := m.Tiers[m.Assign[key]]; !ok {
			add("models.assign."+key, "tier %q is not declared under models.tiers", m.Assign[key])
		}
	}
	for _, name := range slices.Sorted(maps.Keys(m.Tiers)) {
		for _, h := range slices.Sorted(maps.Keys(m.Tiers[name].Efforts)) {
			if !Harness(h).Valid() {
				add("models.tiers."+name+".efforts."+h, "unknown harness %q (want one of %s)", h, harnessList())
			}
		}
	}
}

// validateAutonomy checks the two enum keys of an autonomy table.
func validateAutonomy(where string, a Autonomy, add func(string, string, ...any)) {
	if a.ExecutionMode != "" && !slices.Contains(ExecutionModes(), a.ExecutionMode) {
		add(where+".execution_mode", "unknown value %q (want one of %s)", a.ExecutionMode, strings.Join(ExecutionModes(), ", "))
	}
	if a.SandboxMode != "" && !slices.Contains(SandboxModes(), a.SandboxMode) {
		add(where+".sandbox_mode", "unknown value %q (want one of %s)", a.SandboxMode, strings.Join(SandboxModes(), ", "))
	}
}

// validateReasons checks that every reason names a pattern of a permission
// command list.
func validateReasons(p Permissions, add func(string, string, ...any)) {
	patterns := slices.Concat(p.AllowCommands, p.AllowCommandPatterns, p.DenyCommandPatterns,
		p.DenyCommandExact, p.DenyCommandPrefixes, p.AskCommandPrefixes)
	for _, pat := range slices.Sorted(maps.Keys(p.Reasons)) {
		if !slices.Contains(patterns, pat) {
			add("permissions.reasons."+pat, "matches no pattern of the permission command lists")
		}
	}
}

func harnessList() string {
	names := make([]string, 0, len(All()))
	for _, h := range All() {
		names = append(names, string(h))
	}
	return strings.Join(names, ", ")
}

func validatePlugins(where string, p Plugins, add func(string, string, ...any)) {
	for _, name := range slices.Sorted(maps.Keys(p.Enabled)) {
		for _, h := range p.Enabled[name] {
			if !h.Valid() {
				add(where+".enabled."+name, "unknown harness %q (want one of %s)", h, harnessList())
			}
		}
	}
}

// validateServers checks MCP servers. complete is false for profile overlays,
// where an entry may only add a field to a server declared elsewhere.
func validateServers(where string, servers map[string]MCPServer, complete bool, add func(string, string, ...any)) {
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		s := servers[name]
		w := where + "." + name
		hasURL, hasCmd := s.URL != "", s.Command != ""
		if complete && hasURL == hasCmd {
			add(w, "needs exactly one of url or command")
		}
		o := s.OAuth
		if o.ClientID != "" || o.CallbackPort != 0 || o.PublicClient != nil {
			if complete && !hasURL {
				add(w+".oauth", "is only valid on a url server")
			}
			if o.CallbackPort != 0 && (o.CallbackPort < 1 || o.CallbackPort > 65535) {
				add(w+".oauth.callback_port", "must be 1..65535, got %d", o.CallbackPort)
			}
			if strings.ContainsFunc(o.ClientID, unicode.IsControl) || strings.ContainsAny(o.ClientID, `"'\`) {
				add(w+".oauth.client_id", "must not contain control characters, quotes or backslashes")
			}
		}
	}
}

// ValidateWorkspaces refuses expand_env on a workspace inside a git work
// tree, where the expanded secrets could be committed. The path is expanded
// with home and getenv; the filesystem is read through stat. A path with an
// unset variable is skipped: the renderer skips that workspace too.
func ValidateWorkspaces(c Config, home string, getenv func(string) string, stat func(string) (fs.FileInfo, error)) []finding.Finding {
	var out []finding.Finding
	for _, name := range slices.Sorted(maps.Keys(c.MCP.Workspaces)) {
		w := c.MCP.Workspaces[name]
		if !w.ExpandEnv || strings.TrimSpace(w.Path) == "" {
			continue
		}
		path, unset := ExpandPath(w.Path, home, getenv)
		if len(unset) > 0 {
			continue
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if root, ok := gitWorkTree(path, stat); ok {
			out = append(out, finding.New("mcp.workspaces."+name+".expand_env",
				"refused: %s is inside the git work tree %s, where the expanded secrets could be committed", path, root))
		}
	}
	return out
}

// gitWorkTree returns the first work tree root that holds path, checking the
// path as written and with symlinks resolved on its nearest existing
// ancestor: a link into a repository is inside that work tree.
func gitWorkTree(path string, stat func(string) (fs.FileInfo, error)) (string, bool) {
	if root, ok := workTreeAbove(path, stat); ok {
		return root, true
	}
	existing, tail := path, ""
	for {
		if _, err := stat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", false
		}
		tail = filepath.Join(filepath.Base(existing), tail)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", false
	}
	resolved = filepath.Join(resolved, tail)
	if resolved == path {
		return "", false
	}
	return workTreeAbove(resolved, stat)
}

// workTreeAbove walks up from the nearest existing ancestor of path and
// returns the first directory that holds a .git entry (a directory, or a
// file in a linked work tree).
func workTreeAbove(path string, stat func(string) (fs.FileInfo, error)) (string, bool) {
	dir := path
	for {
		if _, err := stat(dir); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
	for {
		if _, err := stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
