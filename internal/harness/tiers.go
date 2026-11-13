package harness

import (
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/giocaizzi/agents/internal/config"
)

// How each harness applies a model tier. agents never edits plugin files or
// their frontmatter: a per-agent model is written only through a harness's
// own user-level setting, and where there is none the plugin's own model
// applies. There is no translation between model namespaces: a tier names
// the native model id of each harness, and a harness without one keeps its
// plugin or session default.
//
//   - Copilot CLI: per agent, subagents.agents."<plugin>:<agent>" =
//     {model, effortLevel} in ~/.copilot/settings.json. Session: model.
//   - opencode: per agent, agents.<agent>.model = "provider/id" in the user
//     config (the config schema's current key is `agents`; `agent` is
//     deprecated, https://opencode.ai/config.json). The schema has no
//     per-agent effort field, so none is written. <agent> is the file name
//     the registry installer gives the agent in ~/.config/opencode/agents,
//     the bare agent name without the plugin. Session: model.
//   - Claude Code: no settings-level per-subagent model override (only the
//     session `model` and a global subagent environment variable), so the
//     plugin frontmatter pin applies. Session: model and effortLevel.
//   - Codex, pi: no user-level per-agent override without rewriting
//     plugin-generated files. Session: codex model and
//     model_reasoning_effort; pi defaultModel and defaultThinkingLevel.
//   - Antigravity, VS Code: neither per agent nor session.

// keyModel is the model key of every harness setting that takes one.
const keyModel = "model"

// SupportsPerAgent reports whether agents can set a per-agent model for h
// through the harness's own user-level settings.
func SupportsPerAgent(h config.Harness) bool {
	return h == config.Copilot || h == config.OpenCode
}

// SupportsSession reports whether agents sets the main session model of h
// from the default tier.
func SupportsSession(h config.Harness) bool {
	switch h {
	case config.Claude, config.Copilot, config.Codex, config.OpenCode, config.Pi:
		return true
	case config.VSCode, config.Antigravity:
		return false
	}
	return false
}

// DefaultTier returns the tier named by models.default.
func DefaultTier(c *config.Config) (config.Tier, bool) {
	if c.Models.Default == "" {
		return config.Tier{}, false
	}
	t, ok := c.Models.Tiers[c.Models.Default]
	return t, ok
}

// sessionModel is the default tier's model and effort for h, "" when the
// harness has no session setting or the tier has no model for it.
func sessionModel(c *config.Config, h config.Harness) (model, effort string) {
	t, ok := DefaultTier(c)
	if !ok || !SupportsSession(h) {
		return "", ""
	}
	if m := t.Model(h); m != "" {
		return m, t.EffortFor(h)
	}
	return "", ""
}

// isGlob reports whether an assignment key is a pattern.
func isGlob(key string) bool { return strings.ContainsAny(key, "*?[") }

// MatchAgent reports whether the assignment key matches the agent id. A key
// without glob characters matches only itself.
func MatchAgent(key, id string) bool {
	if !isGlob(key) {
		return key == id
	}
	ok, err := path.Match(key, id)
	return err == nil && ok
}

// ResolveAssign maps agent ids to tier names. Every exact key is kept, even
// for an agent not in agents; each listed agent without an exact key takes
// the first matching glob in sorted key order.
func ResolveAssign(c *config.Config, agents []string) map[string]string {
	out := map[string]string{}
	keys := slices.Sorted(maps.Keys(c.Models.Assign))
	for _, k := range keys {
		if !isGlob(k) {
			out[k] = c.Models.Assign[k]
		}
	}
	for _, id := range agents {
		if _, ok := out[id]; ok {
			continue
		}
		for _, k := range keys {
			if isGlob(k) && MatchAgent(k, id) {
				out[id] = c.Models.Assign[k]
				break
			}
		}
	}
	return out
}

// agentTiers is the tier of each assigned agent whose tier has a model for h,
// by agent id; empty when h has no per-agent setting.
func agentTiers(c *config.Config, h config.Harness, agents []string) map[string]config.Tier {
	out := map[string]config.Tier{}
	if !SupportsPerAgent(h) {
		return out
	}
	for id, name := range ResolveAssign(c, agents) {
		if t := c.Models.Tiers[name]; t.Model(h) != "" {
			out[id] = t
		}
	}
	return out
}

// openCodeAgentName is the name opencode gives plugin agent id: the registry
// installer copies .generated/opencode/<agent>.md to agents/<agent>.md.
func openCodeAgentName(id string) string {
	_, name, ok := strings.Cut(id, ":")
	if !ok {
		return id
	}
	return name
}

// InstalledAgents lists the agent ids "<plugin>:<agent>" of the registry
// checkout at dir (plugins/<plugin>/agents/<agent>.md), sorted. The error
// wraps fs.ErrNotExist when the checkout has no plugins directory.
func InstalledAgents(dir string) ([]string, error) {
	if dir == "" {
		return nil, fs.ErrNotExist
	}
	plugins, err := os.ReadDir(filepath.Join(dir, "plugins"))
	if err != nil {
		return nil, err //nolint:wrapcheck // callers only test it against fs.ErrNotExist
	}
	var ids []string
	for _, p := range plugins {
		if !p.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, "plugins", p.Name(), "agents"))
		if err != nil {
			continue
		}
		for _, f := range files {
			if name, ok := strings.CutSuffix(f.Name(), ".md"); ok && !f.IsDir() {
				ids = append(ids, p.Name()+":"+name)
			}
		}
	}
	slices.Sort(ids)
	return ids, nil
}
