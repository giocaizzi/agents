// Package pluginmgmt inspects, updates, and prunes harness plugins.
package pluginmgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/giocaizzi/agents/internal/pluginreg"
)

// PluginState describes the state of a plugin on a harness.
type PluginState int

const (
	// StateDisabled indicates the plugin is not enabled for the harness.
	StateDisabled PluginState = iota
	// StateMissing indicates the plugin is enabled but not installed.
	StateMissing
	// StateStale indicates the installed version is older than upstream.
	StateStale
	// StateOK indicates the installed version matches or exceeds upstream.
	StateOK
)

const (
	harnessAntigravity = "antigravity"
	harnessClaude      = "claude"
	harnessCodex       = "codex"
	harnessCopilot     = "copilot"
	harnessOpencode    = "opencode"
	harnessPi          = "pi"
)

// fetchTimeout bounds the git fetch of a registry checkout's remote tip.
const fetchTimeout = 10 * time.Second

// PluginInfo records upstream and per-harness status for one plugin.
type PluginInfo struct {
	Name            string            `json:"name"`
	UpstreamVersion string            `json:"upstream_version"`
	UpstreamStale   bool              `json:"upstream_stale,omitempty"`
	IsStandalone    bool              `json:"is_standalone,omitempty"`
	Source          string            `json:"source,omitempty"`
	Enabled         map[string]bool   `json:"enabled"`
	Installed       map[string]string `json:"installed"`
}

// State returns the PluginState for harness.
func (p PluginInfo) State(harness string) PluginState {
	if !p.Enabled[harness] {
		return StateDisabled
	}
	v, ok := p.Installed[harness]
	if !ok || v == "" {
		return StateMissing
	}
	// An install without a version ("linked", "installed", "enabled") cannot
	// be older than upstream.
	if p.UpstreamVersion == "" || !isVersion(v) {
		return StateOK
	}
	if CompareVersions(v, p.UpstreamVersion) < 0 {
		return StateStale
	}
	return StateOK
}

// Behind reports whether the plugin is stale or missing on harness.
func (p PluginInfo) Behind(harness string) bool {
	s := p.State(harness)
	return s == StateStale || s == StateMissing
}

// isVersion reports whether v is a semantic version, with or without a "v" prefix.
func isVersion(v string) bool {
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return semver.IsValid(v)
}

// PruneCandidate identifies a directory that can be safely pruned.
type PruneCandidate struct {
	Harness string
	Plugin  string
	Version string
	Path    string
	Reason  string
}

// Manager inspects and manages plugins across harnesses.
type Manager struct {
	HarnessDir    string
	HomeDir       string
	Registry      string
	Namespace     string
	HarnessConfig pluginreg.HarnessConfig
}

func (m Manager) namespace() string {
	if m.Namespace != "" {
		return m.Namespace
	}
	reg := m.Registry
	if reg == "" {
		reg = m.HarnessConfig.Registry
	}
	if reg != "" {
		return pluginreg.ResolveNamespace(reg)
	}
	if m.HarnessDir != "" {
		if discovered := pluginreg.DiscoverMarketplaceNamespace(m.HarnessDir); discovered != "" {
			return discovered
		}
	}
	if reg == "" {
		reg = pluginreg.DefaultHarnessRegistry
	}
	return pluginreg.ResolveNamespace(reg)
}

// EffectiveNamespace returns the active namespace for the manager.
func (m Manager) EffectiveNamespace() string {
	return m.namespace()
}

// CompareVersions compares two semver strings (with or without 'v' prefix).
func CompareVersions(v1, v2 string) int {
	clean1 := v1
	if !strings.HasPrefix(clean1, "v") {
		clean1 = "v" + clean1
	}
	clean2 := v2
	if !strings.HasPrefix(clean2, "v") {
		clean2 = "v" + clean2
	}
	if semver.IsValid(clean1) && semver.IsValid(clean2) {
		return semver.Compare(clean1, clean2)
	}
	return strings.Compare(v1, v2)
}

// versionCandidates lists the manifest paths, relative to a plugin directory,
// that may carry its version, in lookup order.
func versionCandidates() []string {
	return []string{
		"plugin.json",
		path.Join(".antigravity-plugin", "plugin.json"),
		path.Join(".claude-plugin", "plugin.json"),
	}
}

// parseVersion extracts the version field from plugin manifest JSON.
func parseVersion(data []byte) string {
	var meta struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	return meta.Version
}

// ReadPluginVersion reads the version field from a plugin directory.
func ReadPluginVersion(dir string) string {
	for _, cand := range versionCandidates() {
		data, err := os.ReadFile(filepath.Clean(filepath.Join(dir, filepath.FromSlash(cand))))
		if err != nil {
			continue
		}
		if v := parseVersion(data); v != "" {
			return v
		}
	}
	return ""
}

// gitEnv returns the process environment with interactive prompts disabled.
func gitEnv() []string {
	return append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
}

// fetchRemoteTip fetches the remote HEAD of the git checkout at dir into
// FETCH_HEAD without touching the working tree.
func fetchRemoteTip(ctx context.Context, dir string) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "fetch", "--quiet", "origin", "HEAD")
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch in %s: %w: %s", dir, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// readFetchedVersion reads a plugin version from FETCH_HEAD of the checkout at dir.
func readFetchedVersion(ctx context.Context, dir, name string) string {
	for _, cand := range versionCandidates() {
		cmd := exec.CommandContext(ctx, "git", "-C", dir, "show", "FETCH_HEAD:"+path.Join("plugins", name, cand))
		cmd.Env = gitEnv()
		data, err := cmd.Output()
		if err != nil {
			continue
		}
		if v := parseVersion(data); v != "" {
			return v
		}
	}
	return ""
}

// upstreamVersions returns the upstream version of each named registry plugin.
// When dir is a git checkout it fetches the remote tip and reads versions from
// it, leaving the working tree untouched; if the fetch fails it falls back to
// the working tree and reports fresh=false. A plain directory is read as is.
func upstreamVersions(ctx context.Context, dir string, names []string) (map[string]string, bool) {
	out := make(map[string]string, len(names))
	fresh := true
	useGit := false
	if dir != "" {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			if fetchRemoteTip(ctx, dir) == nil {
				useGit = true
			} else {
				fresh = false
			}
		}
	}
	for _, name := range names {
		if useGit {
			out[name] = readFetchedVersion(ctx, dir, name)
		} else {
			out[name] = ReadPluginVersion(filepath.Join(dir, "plugins", name))
		}
	}
	return out, fresh
}

// ListPlugins inspects all configured plugins and their status per harness.
func (m Manager) ListPlugins(ctx context.Context) ([]PluginInfo, error) {
	upstreamNames, err := pluginreg.ResolveUpstreamRegistry(ctx, m.HarnessDir, m.Registry)
	if err != nil {
		upstreamNames = make([]string, 0, len(m.HarnessConfig.Plugins))
		for p := range m.HarnessConfig.Plugins {
			upstreamNames = append(upstreamNames, p)
		}
		sort.Strings(upstreamNames)
	}

	versions, fresh := upstreamVersions(ctx, m.HarnessDir, upstreamNames)
	out := make([]PluginInfo, 0, len(upstreamNames)+len(m.HarnessConfig.Standalone))

	for _, name := range upstreamNames {
		info := PluginInfo{
			Name:            name,
			UpstreamVersion: versions[name],
			UpstreamStale:   !fresh,
			Enabled:         make(map[string]bool),
			Installed:       make(map[string]string),
		}

		if hs, ok := m.HarnessConfig.Plugins[name]; ok {
			for _, h := range hs {
				info.Enabled[h] = true
			}
		}

		info.Installed[harnessAntigravity] = m.antigravityVersion(name)
		info.Installed[harnessClaude] = m.claudeVersion(name)
		info.Installed[harnessCodex] = m.codexVersion(name)
		info.Installed[harnessCopilot] = m.copilotVersion(name)
		info.Installed[harnessOpencode] = m.opencodeVersion(name)
		info.Installed[harnessPi] = m.piVersion(name)

		out = append(out, info)
	}

	// List standalone plugins
	for name, s := range m.HarnessConfig.Standalone {
		upVer := s.Version
		if upVer == "" && s.Path != "" {
			upVer = ReadPluginVersion(s.Path)
		}
		info := PluginInfo{
			Name:            name,
			UpstreamVersion: upVer,
			IsStandalone:    true,
			Source:          s.Source,
			Enabled:         make(map[string]bool),
			Installed:       make(map[string]string),
		}
		for _, h := range s.Harnesses {
			info.Enabled[h] = true
		}
		info.Installed[harnessAntigravity] = m.antigravityVersion(name)
		info.Installed[harnessClaude] = m.claudeVersion(name)
		info.Installed[harnessCodex] = m.codexVersion(name)
		info.Installed[harnessCopilot] = m.copilotVersion(name)
		info.Installed[harnessOpencode] = m.opencodeVersion(name)
		info.Installed[harnessPi] = m.piVersion(name)
		out = append(out, info)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

const statusInstalled = "installed"

func (m Manager) antigravityVersion(plugin string) string {
	pDir := filepath.Join(m.HomeDir, ".gemini", "config", "plugins", plugin)
	if _, err := os.Stat(pDir); err != nil {
		return ""
	}
	v := ReadPluginVersion(pDir)
	if v != "" {
		return v
	}
	return statusInstalled
}

func (m Manager) claudeVersion(plugin string) string {
	cacheDir := filepath.Join(m.HomeDir, ".claude", "plugins", "cache", m.namespace(), plugin)
	return newestDirVersion(cacheDir)
}

func (m Manager) codexVersion(plugin string) string {
	cacheDir := filepath.Join(m.HomeDir, ".codex", "plugins", "cache", m.namespace(), plugin)
	return newestDirVersion(cacheDir)
}

func newestDirVersion(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			versions = append(versions, e.Name())
		}
	}
	if len(versions) == 0 {
		return ""
	}
	sort.Slice(versions, func(i, j int) bool {
		return CompareVersions(versions[i], versions[j]) > 0
	})
	return versions[0]
}

func (m Manager) copilotInstalledDirVersion(ns, plugin string) (string, bool) {
	installedDir := filepath.Join(m.HomeDir, ".copilot", "installed-plugins", ns, plugin)
	if _, err := os.Stat(installedDir); err == nil {
		return ReadPluginVersion(installedDir), true
	}
	return "", false
}

func (m Manager) copilotConfigFileVersion(ns, plugin string) string {
	configFile := filepath.Join(m.HomeDir, ".copilot", "config.json")
	data, err := os.ReadFile(filepath.Clean(configFile))
	if err != nil {
		return ""
	}
	var cfg struct {
		InstalledPlugins []struct {
			Name        string `json:"name"`
			Version     string `json:"version"`
			Marketplace string `json:"marketplace"`
			Enabled     bool   `json:"enabled"`
		} `json:"installedPlugins"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	for _, p := range cfg.InstalledPlugins {
		if (p.Name == plugin || p.Name == plugin+"@"+ns) && (p.Marketplace == "" || p.Marketplace == ns) {
			if p.Version != "" {
				return p.Version
			}
			if p.Enabled {
				return "enabled"
			}
			return statusInstalled
		}
	}
	return ""
}

func (m Manager) vscodeAgentPluginsVersion(ns, plugin string) string {
	base := filepath.Join(m.HomeDir, ".vscode", "agent-plugins")
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	needle := ns + "-" + plugin
	for _, e := range entries {
		if e.IsDir() && strings.Contains(e.Name(), needle) {
			v := ReadPluginVersion(filepath.Join(base, e.Name()))
			if v != "" {
				return v
			}
			return statusInstalled
		}
	}
	return ""
}

func (m Manager) copilotSettingsVersion(ns, plugin string) string {
	settingsFile := filepath.Join(m.HomeDir, ".copilot", "settings.json")
	data, err := os.ReadFile(filepath.Clean(settingsFile))
	if err != nil {
		return ""
	}
	var s struct {
		EnabledPlugins map[string]bool `json:"enabledPlugins"`
	}
	if err := json.Unmarshal(data, &s); err == nil && s.EnabledPlugins[plugin+"@"+ns] {
		return "enabled"
	}
	return ""
}

func (m Manager) copilotVersion(plugin string) string {
	ns := m.namespace()

	dirVer, dirExists := m.copilotInstalledDirVersion(ns, plugin)
	if dirVer != "" {
		return dirVer
	}

	if cfgVer := m.copilotConfigFileVersion(ns, plugin); cfgVer != "" {
		return cfgVer
	}

	if dirExists {
		return statusInstalled
	}

	if vsVer := m.vscodeAgentPluginsVersion(ns, plugin); vsVer != "" {
		return vsVer
	}

	return m.copilotSettingsVersion(ns, plugin)
}

func (m Manager) opencodeVersion(_ string) string {
	skillsDir := filepath.Join(m.HomeDir, ".config", "opencode", "skills")
	if _, err := os.Stat(skillsDir); err == nil {
		return "linked"
	}
	return ""
}

func (m Manager) piVersion(_ string) string {
	piDir := filepath.Join(m.HomeDir, ".pi")
	if _, err := os.Stat(piDir); err == nil {
		return "linked"
	}
	return ""
}

func findCachePruneCandidates(baseDir, harnessName string) []PruneCandidate {
	var candidates []PruneCandidate
	plugins, err := os.ReadDir(baseDir)
	if err != nil {
		return candidates
	}
	for _, p := range plugins {
		if !p.IsDir() {
			continue
		}
		pDir := filepath.Join(baseDir, p.Name())
		versions, err := os.ReadDir(pDir)
		if err != nil {
			continue
		}
		var dirNames []string
		for _, v := range versions {
			if v.IsDir() && !strings.HasPrefix(v.Name(), ".") {
				dirNames = append(dirNames, v.Name())
			}
		}
		if len(dirNames) > 1 {
			sort.Slice(dirNames, func(i, j int) bool {
				return CompareVersions(dirNames[i], dirNames[j]) > 0
			})
			for _, oldVer := range dirNames[1:] {
				candidates = append(candidates, PruneCandidate{
					Harness: harnessName,
					Plugin:  p.Name(),
					Version: oldVer,
					Path:    filepath.Join(pDir, oldVer),
					Reason:  "superseded by " + dirNames[0],
				})
			}
		}
	}
	return candidates
}

// FindPruneCandidates searches for obsolete plugin cache directories or orphaned plugins.
func (m Manager) FindPruneCandidates() ([]PruneCandidate, error) {
	var candidates []PruneCandidate

	// 1. Claude cache
	claudeBase := filepath.Join(m.HomeDir, ".claude", "plugins", "cache", m.namespace())
	candidates = append(candidates, findCachePruneCandidates(claudeBase, harnessClaude)...)

	// 2. Codex cache
	codexBase := filepath.Join(m.HomeDir, ".codex", "plugins", "cache", m.namespace())
	candidates = append(candidates, findCachePruneCandidates(codexBase, harnessCodex)...)

	// 3. Antigravity orphaned plugins
	agyDir := filepath.Join(m.HomeDir, ".gemini", "config", "plugins")
	if plugins, err := os.ReadDir(agyDir); err == nil {
		for _, p := range plugins {
			if !p.IsDir() || strings.HasPrefix(p.Name(), ".") {
				continue
			}
			name := p.Name()
			if _, ok := m.HarnessConfig.Plugins[name]; !ok && name == "api" {
				candidates = append(candidates, PruneCandidate{
					Harness: harnessAntigravity,
					Plugin:  name,
					Path:    filepath.Join(agyDir, name),
					Reason:  "obsolete plugin replaced upstream",
				})
			}
		}
	}

	return candidates, nil
}

// Prune removes the directories identified by FindPruneCandidates.
func (m Manager) Prune(candidates []PruneCandidate) ([]string, error) {
	var pruned []string
	for _, c := range candidates {
		if err := os.RemoveAll(c.Path); err != nil {
			return pruned, fmt.Errorf("remove %s: %w", c.Path, err)
		}
		pruned = append(pruned, c.Path)
	}
	return pruned, nil
}

// EnabledHarnesses returns the distinct list of harnesses configured.
func EnabledHarnesses(cfg pluginreg.HarnessConfig) []string {
	seen := make(map[string]bool)
	for _, hs := range cfg.Plugins {
		for _, h := range hs {
			seen[h] = true
		}
	}
	out := make([]string, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// DefaultHarnessOrder returns the canonical ordering of harnesses for display.
func DefaultHarnessOrder() []string {
	return []string{harnessAntigravity, harnessClaude, harnessCopilot, harnessCodex, harnessOpencode, harnessPi}
}

// SortHarnesses sorts a list of harness names according to the canonical order.
func SortHarnesses(harnesses []string) []string {
	res := make([]string, len(harnesses))
	copy(res, harnesses)
	order := DefaultHarnessOrder()
	sort.Slice(res, func(i, j int) bool {
		idxI := slices.Index(order, res[i])
		idxJ := slices.Index(order, res[j])
		if idxI >= 0 && idxJ >= 0 {
			return idxI < idxJ
		}
		if idxI >= 0 {
			return true
		}
		if idxJ >= 0 {
			return false
		}
		return res[i] < res[j]
	})
	return res
}
