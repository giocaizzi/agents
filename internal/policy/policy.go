// Package policy parses, validates and holds canonical AI harness configurations
// across Claude Code, Antigravity, Copilot CLI, VS Code Copilot, Codex, Pi, and opencode.
package policy

import "maps"

// Policy is the canonical cross-harness security, notification and runtime configuration.
type Policy struct {
	Autonomy      Autonomy           `json:"autonomy"           toml:"autonomy"`
	Permissions   Permissions        `json:"permissions"        toml:"permissions"`
	Models        Models             `json:"models"             toml:"models"`
	Context       Context            `json:"context"            toml:"context"`
	UI            UI                 `json:"ui"                 toml:"ui"`
	Notifications Notifications      `json:"notifications"      toml:"notifications"`
	Telemetry     Telemetry          `json:"telemetry"          toml:"telemetry"`
	LSP           LSP                `json:"lsp"                toml:"lsp"`
	Voice         Voice              `json:"voice"              toml:"voice"`
	Native        Native             `json:"native"             toml:"native"`
	Profiles      map[string]Profile `json:"profiles,omitempty" toml:"profiles,omitempty"`
	MCP           map[string]any     `json:"mcp,omitempty"      toml:"mcp,omitempty"`
}

// Profile configures layered prompt instructions and optional configuration overrides.
type Profile struct {
	Instructions []string     `json:"instructions,omitempty" toml:"instructions,omitempty"`
	Models       *Models      `json:"models,omitempty"       toml:"models,omitempty"`
	Permissions  *Permissions `json:"permissions,omitempty"  toml:"permissions,omitempty"`
	Autonomy     *Autonomy    `json:"autonomy,omitempty"     toml:"autonomy,omitempty"`
	UI           *UI          `json:"ui,omitempty"           toml:"ui,omitempty"`
	Plugins      []string     `json:"plugins,omitempty"      toml:"plugins,omitempty"`
}

// Autonomy configures agent autonomy level.
type Autonomy struct {
	ExecutionMode            string `json:"executionMode,omitempty"            toml:"executionMode,omitempty"`
	SandboxMode              string `json:"sandboxMode,omitempty"              toml:"sandboxMode,omitempty"`
	AllowNonWorkspaceAccess  bool   `json:"allowNonWorkspaceAccess,omitempty"  toml:"allowNonWorkspaceAccess,omitempty"`
	SubagentDepth            int    `json:"subagentDepth,omitempty"            toml:"subagentDepth,omitempty"`
	AllowSubagentInvocations bool   `json:"allowSubagentInvocations,omitempty" toml:"allowSubagentInvocations,omitempty"`
	AskTimeout               string `json:"askTimeout,omitempty"               toml:"askTimeout,omitempty"`
	Mode                     string `json:"mode,omitempty"                     toml:"mode,omitempty"`
}

// Permissions defines allowed, forbidden and sensitive resources across all harnesses.
type Permissions struct {
	AllowCommands        []string `json:"allowCommands"        toml:"allowCommands"`
	AllowedUrls          []string `json:"allowedUrls"          toml:"allowedUrls"`
	AllowCommandPatterns []string `json:"allowCommandPatterns" toml:"allowCommandPatterns"`
	DenyRead             []string `json:"denyRead"             toml:"denyRead"`
	DenyWrite            []string `json:"denyWrite"            toml:"denyWrite"`
	DenyCommandPatterns  []string `json:"denyCommandPatterns"  toml:"denyCommandPatterns"`
	DenyCommandExact     []string `json:"denyCommandExact"     toml:"denyCommandExact"`
	DenyCommandPrefixes  []string `json:"denyCommandPrefixes"  toml:"denyCommandPrefixes"`
	AskCommandPrefixes   []string `json:"askCommandPrefixes"   toml:"askCommandPrefixes"`
	AllowedMCP           []string `json:"allowedMcp,omitempty" toml:"allowedMcp,omitempty"`
}

// Models holds model tier mappings across agents and catalog preferences.
type Models struct {
	DefaultEffortLevel    string               `json:"defaultEffortLevel,omitempty"    toml:"defaultEffortLevel,omitempty"`
	ShowThinkingSummaries bool                 `json:"showThinkingSummaries,omitempty" toml:"showThinkingSummaries,omitempty"`
	Tiers                 map[string]ModelTier `json:"tiers,omitempty"                 toml:"tiers,omitempty"`
	Catalog               CatalogConfig        `json:"catalog"                         toml:"catalog,omitempty"`
}

// CatalogConfig defines manual catalog sync preferences.
type CatalogConfig struct {
	Providers []string                     `json:"providers,omitempty" toml:"providers,omitempty"`
	Since     string                       `json:"since,omitempty"     toml:"since,omitempty"`
	Pins      map[string]string            `json:"pins,omitempty"      toml:"pins,omitempty"`
	Ignore    []string                     `json:"ignore,omitempty"    toml:"ignore,omitempty"`
	Aliases   map[string]map[string]string `json:"aliases,omitempty"   toml:"aliases,omitempty"`
}

// ModelTier specifies model and effort for a tier.
type ModelTier struct {
	Model  string `json:"model"  toml:"model"`
	Effort string `json:"effort" toml:"effort"`
}

// Context configures prompt context and window compaction settings.
type Context struct {
	AutoCompact            bool   `json:"autoCompact,omitempty"            toml:"autoCompact,omitempty"`
	AutoCompactWindow      int    `json:"autoCompactWindow,omitempty"      toml:"autoCompactWindow,omitempty"`
	PromptCacheTtl         string `json:"promptCacheTtl,omitempty"         toml:"promptCacheTtl,omitempty"`
	SubagentPromptCacheTtl string `json:"subagentPromptCacheTtl,omitempty" toml:"subagentPromptCacheTtl,omitempty"`
	MaxTurnTokens          int    `json:"maxTurnTokens,omitempty"          toml:"maxTurnTokens,omitempty"`
	MaxTokens              int    `json:"maxTokens,omitempty"              toml:"maxTokens,omitempty"`
}

// UI configures appearance, notifications, and bell settings.
type UI struct {
	TerminalBell     bool   `json:"terminalBell,omitempty"     toml:"terminalBell,omitempty"`
	Theme            string `json:"theme,omitempty"            toml:"theme,omitempty"`
	TuiMode          string `json:"tuiMode,omitempty"          toml:"tuiMode,omitempty"`
	ViewMode         string `json:"viewMode,omitempty"         toml:"viewMode,omitempty"`
	OutputStyle      string `json:"outputStyle,omitempty"      toml:"outputStyle,omitempty"`
	ShowTurnDuration bool   `json:"showTurnDuration,omitempty" toml:"showTurnDuration,omitempty"`
	ProgressBar      bool   `json:"progressBar,omitempty"      toml:"progressBar,omitempty"`
	TimeFormat       string `json:"timeFormat,omitempty"       toml:"timeFormat,omitempty"`
}

// Notifications configures shared notification preferences across harnesses.
type Notifications struct {
	TerminalBell bool `json:"terminalBell" toml:"terminalBell"`
}

// Telemetry configures dual-tier metrics, tracing and hooks bridge.
type Telemetry struct {
	Enabled          bool              `json:"enabled,omitempty"          toml:"enabled,omitempty"`
	DualTier         bool              `json:"dualTier,omitempty"         toml:"dualTier,omitempty"`
	Protocol         string            `json:"protocol,omitempty"         toml:"protocol,omitempty"`
	Temporality      string            `json:"temporality,omitempty"      toml:"temporality,omitempty"`
	LogToolDetails   bool              `json:"logToolDetails,omitempty"   toml:"logToolDetails,omitempty"`
	LogToolContent   bool              `json:"logToolContent,omitempty"   toml:"logToolContent,omitempty"`
	ServiceNamespace string            `json:"serviceNamespace,omitempty" toml:"serviceNamespace,omitempty"`
	Attributes       map[string]string `json:"attributes,omitempty"       toml:"attributes,omitempty"`
	Services         map[string]string `json:"services,omitempty"         toml:"services,omitempty"`
	HooksBridge      bool              `json:"hooksBridge,omitempty"      toml:"hooksBridge,omitempty"`
}

// LSP configures Language Server Protocol tool integrations: one server entry
// per language server, rendered into every harness and editor that takes custom servers.
type LSP struct {
	Enabled bool                 `json:"enabled,omitempty" toml:"enabled,omitempty"`
	Server  map[string]LSPServer `json:"server,omitempty"  toml:"server,omitempty"`
}

// LSPServer describes one language server: its packages.toml row, the argv that
// starts it on stdio, the file extensions it serves, the Vim filetypes it attaches to and whether editors format with it.
type LSPServer struct {
	Package    string            `json:"package,omitempty"    toml:"package,omitempty"`
	Command    []string          `json:"command,omitempty"    toml:"command,omitempty"`
	Extensions map[string]string `json:"extensions,omitempty" toml:"extensions,omitempty"`
	Filetypes  []string          `json:"filetypes,omitempty"  toml:"filetypes,omitempty"`
	Format     bool              `json:"format,omitempty"     toml:"format,omitempty"`
}

// Voice configures dictation (voice input) where a harness supports it natively.
type Voice struct {
	Enabled    bool   `json:"enabled,omitempty"    toml:"enabled,omitempty"`
	Mode       string `json:"mode,omitempty"       toml:"mode,omitempty"`
	AutoSubmit bool   `json:"autoSubmit,omitempty" toml:"autoSubmit,omitempty"`
	Language   string `json:"language,omitempty"   toml:"language,omitempty"`
}

// Native holds harness-specific configuration settings and overrides.
type Native struct {
	Claude      ClaudeNative      `json:"claude"      toml:"claude"`
	Antigravity AntigravityNative `json:"antigravity" toml:"antigravity"`
	Codex       CodexNative       `json:"codex"       toml:"codex"`
	Copilot     CopilotNative     `json:"copilot"     toml:"copilot"`
	Pi          PiNative          `json:"pi"          toml:"pi"`
	OpenCode    OpenCodeNative    `json:"opencode"    toml:"opencode"`
}

// ClaudeNative defines Claude Code native settings.
type ClaudeNative struct {
	DefaultMode               string `json:"defaultMode,omitempty"               toml:"defaultMode,omitempty"`
	SkipAutoPermissionPrompt  bool   `json:"skipAutoPermissionPrompt,omitempty"  toml:"skipAutoPermissionPrompt,omitempty"`
	CleanupPeriodDays         int    `json:"cleanupPeriodDays,omitempty"         toml:"cleanupPeriodDays,omitempty"`
	DisableClaudeAiConnectors bool   `json:"disableClaudeAiConnectors,omitempty" toml:"disableClaudeAiConnectors,omitempty"`
}

// AntigravityNative defines Antigravity native settings.
type AntigravityNative struct {
	AgentMode               string `json:"agentMode,omitempty"               toml:"agentMode,omitempty"`
	ToolPermission          string `json:"toolPermission,omitempty"          toml:"toolPermission,omitempty"`
	EnableTerminalSandbox   bool   `json:"enableTerminalSandbox,omitempty"   toml:"enableTerminalSandbox,omitempty"`
	AllowNonWorkspaceAccess bool   `json:"allowNonWorkspaceAccess,omitempty" toml:"allowNonWorkspaceAccess,omitempty"`
}

// CodexNative defines optional Codex preferences.
type CodexNative struct {
	Model                *string `json:"model,omitempty"                  toml:"model,omitempty"`
	ModelReasoningEffort *string `json:"model_reasoning_effort,omitempty" toml:"model_reasoning_effort,omitempty"`
	ApprovalPolicy       *string `json:"approval_policy,omitempty"        toml:"approval_policy,omitempty"`
	SandboxMode          *string `json:"sandbox_mode,omitempty"           toml:"sandbox_mode,omitempty"`
}

// CopilotNative defines Copilot CLI native agent bindings.
type CopilotNative struct {
	Agents map[string]AgentBinding `json:"agents,omitempty" toml:"agents,omitempty"`
}

// AgentBinding specifies a model and reasoning effort for an agent.
type AgentBinding struct {
	Model       string `json:"model"       toml:"model"`
	EffortLevel string `json:"effortLevel" toml:"effortLevel"`
}

// PiNative defines native settings for the Pi coding agent.
type PiNative struct {
	DefaultProvider      string   `json:"defaultProvider,omitempty"      toml:"defaultProvider,omitempty"`
	DefaultModel         string   `json:"defaultModel,omitempty"         toml:"defaultModel,omitempty"`
	DefaultThinkingLevel string   `json:"defaultThinkingLevel,omitempty" toml:"defaultThinkingLevel,omitempty"`
	Packages             []string `json:"packages,omitempty"             toml:"packages,omitempty"`
}

// OpenCodeNative defines native settings for opencode.
type OpenCodeNative struct {
	DefaultModel string `json:"defaultModel,omitempty" toml:"defaultModel,omitempty"`
	Sandbox      bool   `json:"sandbox,omitempty"      toml:"sandbox,omitempty"`
}

// WithProfile returns a copy of Policy with the specified profile applied.
// If name is empty, it returns the policy unchanged.
// If the profile exists in Profiles, its non-nil elements override or merge with the base policy.
func (p Policy) WithProfile(name string) *Policy {
	if name == "" {
		return &p
	}
	prof, ok := p.Profiles[name]
	if !ok {
		return &p
	}
	cp := p
	applyModelsProfile(&cp, prof.Models)
	applyPermissionsProfile(&cp, prof.Permissions)
	if prof.Autonomy != nil {
		if prof.Autonomy.ExecutionMode != "" {
			cp.Autonomy.ExecutionMode = prof.Autonomy.ExecutionMode
		}
		if prof.Autonomy.SandboxMode != "" {
			cp.Autonomy.SandboxMode = prof.Autonomy.SandboxMode
		}
	}
	if prof.UI != nil {
		cp.UI = *prof.UI
	}
	return &cp
}

func applyModelsProfile(p *Policy, m *Models) {
	if m == nil || m.Tiers == nil {
		return
	}
	if p.Models.Tiers == nil {
		p.Models.Tiers = make(map[string]ModelTier)
	} else {
		p.Models.Tiers = maps.Clone(p.Models.Tiers)
	}
	maps.Copy(p.Models.Tiers, m.Tiers)
}

func applyPermissionsProfile(p *Policy, perm *Permissions) {
	if perm == nil {
		return
	}
	p.Permissions.AllowCommands = append(p.Permissions.AllowCommands, perm.AllowCommands...)
	p.Permissions.AllowedUrls = append(p.Permissions.AllowedUrls, perm.AllowedUrls...)
	p.Permissions.DenyRead = append(p.Permissions.DenyRead, perm.DenyRead...)
	p.Permissions.DenyWrite = append(p.Permissions.DenyWrite, perm.DenyWrite...)
	p.Permissions.DenyCommandExact = append(p.Permissions.DenyCommandExact, perm.DenyCommandExact...)
	p.Permissions.DenyCommandPrefixes = append(p.Permissions.DenyCommandPrefixes, perm.DenyCommandPrefixes...)
	p.Permissions.DenyCommandPatterns = append(p.Permissions.DenyCommandPatterns, perm.DenyCommandPatterns...)
	p.Permissions.AllowedMCP = append(p.Permissions.AllowedMCP, perm.AllowedMCP...)
}
