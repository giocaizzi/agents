// Package config loads the standalone agents configuration: one strict TOML
// file layered with its OS overlay, a profile overlay, the environment and
// flags. Descriptions in the jsonschema tags feed the generated JSON Schema, so
// they carry no commas.
package config

import "slices"

// Harness names one supported AI harness.
type Harness string

// The supported harnesses.
const (
	Claude      Harness = "claude"
	Copilot     Harness = "copilot"
	VSCode      Harness = "vscode"
	Codex       Harness = "codex"
	OpenCode    Harness = "opencode"
	Pi          Harness = "pi"
	Antigravity Harness = "antigravity"
)

// All returns every harness in canonical order.
func All() []Harness {
	return []Harness{Claude, Copilot, VSCode, Codex, OpenCode, Pi, Antigravity}
}

// Valid reports whether h is a supported harness.
func (h Harness) Valid() bool {
	return slices.Contains(All(), h)
}

// Config is the whole configuration file. Every field is optional.
type Config struct {
	SchemaVersion int                `json:"schema_version,omitempty" jsonschema:"enum=1,description=Schema version of this file (must be 1)"       toml:"schema_version,omitempty"`
	Profile       string             `json:"profile,omitempty"        jsonschema:"description=Selected profile (a machine fact)"                    toml:"profile,omitempty"`
	Harnesses     []Harness          `json:"harnesses,omitempty"      jsonschema:"description=Harnesses to configure (unset means auto-detect)"     toml:"harnesses,omitempty"`
	Autonomy      Autonomy           `json:"autonomy,omitzero"        jsonschema:"description=How freely agents act"                                toml:"autonomy,omitzero"`
	Permissions   Permissions        `json:"permissions,omitzero"     jsonschema:"description=Command and path permissions shared by all harnesses" toml:"permissions,omitzero"`
	Models        Models             `json:"models,omitzero"          jsonschema:"description=Model tiers and the model catalog preferences"        toml:"models,omitzero"`
	Context       Context            `json:"context,omitzero"         jsonschema:"description=Context window and prompt cache behaviour"            toml:"context,omitzero"`
	UI            UI                 `json:"ui,omitzero"              jsonschema:"description=Terminal UI preferences"                              toml:"ui,omitzero"`
	Notifications Notifications      `json:"notifications,omitzero"   jsonschema:"description=Notification preferences"                             toml:"notifications,omitzero"`
	Voice         Voice              `json:"voice,omitzero"           jsonschema:"description=Voice input preferences"                              toml:"voice,omitzero"`
	Telemetry     Telemetry          `json:"telemetry,omitzero"       jsonschema:"description=OpenTelemetry export settings"                        toml:"telemetry,omitzero"`
	LSP           LSP                `json:"lsp,omitzero"             jsonschema:"description=Language servers made available to the harnesses"     toml:"lsp,omitzero"`
	Instructions  Instructions       `json:"instructions,omitzero"    jsonschema:"description=Instruction layers and how to edit them"              toml:"instructions,omitzero"`
	Plugins       Plugins            `json:"plugins,omitzero"         jsonschema:"description=Plugin registry and per-harness plugin selection"     toml:"plugins,omitzero"`
	MCP           MCP                `json:"mcp,omitzero"             jsonschema:"description=MCP servers and workspaces"                           toml:"mcp,omitzero"`
	Harness       HarnessTable       `json:"harness,omitzero"         jsonschema:"description=Per-harness settings"                                 toml:"harness,omitzero"`
	OS            OSTable            `json:"os,omitzero"              jsonschema:"description=Overlays applied on one operating system"             toml:"os,omitzero"`
	Profiles      map[string]Profile `json:"profiles,omitempty"       jsonschema:"description=Named profiles (overlays on top of the config)"       toml:"profiles,omitempty"`
}

// Profile is an overlay selected by name. Its instructions lists are
// composition data and never merge into the config.
type Profile struct {
	Instructions  []string      `json:"instructions,omitempty" jsonschema:"description=Instruction layers composed for this profile (in order)"                            toml:"instructions,omitempty"`
	Autonomy      Autonomy      `json:"autonomy,omitzero"      jsonschema:"description=Autonomy overlay"                                                                   toml:"autonomy,omitzero"`
	Permissions   Permissions   `json:"permissions,omitzero"   jsonschema:"description=Permissions overlay"                                                                toml:"permissions,omitzero"`
	Models        Models        `json:"models,omitzero"        jsonschema:"description=Models overlay"                                                                     toml:"models,omitzero"`
	Context       Context       `json:"context,omitzero"       jsonschema:"description=Context overlay"                                                                    toml:"context,omitzero"`
	UI            UI            `json:"ui,omitzero"            jsonschema:"description=UI overlay"                                                                         toml:"ui,omitzero"`
	Notifications Notifications `json:"notifications,omitzero" jsonschema:"description=Notifications overlay"                                                              toml:"notifications,omitzero"`
	Voice         Voice         `json:"voice,omitzero"         jsonschema:"description=Voice overlay"                                                                      toml:"voice,omitzero"`
	Telemetry     Telemetry     `json:"telemetry,omitzero"     jsonschema:"description=Telemetry overlay"                                                                  toml:"telemetry,omitzero"`
	LSP           LSP           `json:"lsp,omitzero"           jsonschema:"description=LSP overlay"                                                                        toml:"lsp,omitzero"`
	Plugins       Plugins       `json:"plugins,omitzero"       jsonschema:"description=Plugins overlay"                                                                    toml:"plugins,omitzero"`
	MCP           MCP           `json:"mcp,omitzero"           jsonschema:"description=MCP overlay"                                                                        toml:"mcp,omitzero"`
	Harness       HarnessTable  `json:"harness,omitzero"       jsonschema:"description=Per-harness overlay (harness.<h>.instructions is the profile x harness layer list)" toml:"harness,omitzero"`
}

// OSTable has one overlay per operating system. The loader applies the one for
// the running GOOS after the file and before the profile.
type OSTable struct {
	Darwin  Overlay `json:"darwin,omitzero"  jsonschema:"description=Overlay on macOS"   toml:"darwin,omitzero"`
	Linux   Overlay `json:"linux,omitzero"   jsonschema:"description=Overlay on Linux"   toml:"linux,omitzero"`
	Windows Overlay `json:"windows,omitzero" jsonschema:"description=Overlay on Windows" toml:"windows,omitzero"`
}

// Overlay is a profile without instruction lists.
type Overlay struct {
	Autonomy      Autonomy      `json:"autonomy,omitzero"      jsonschema:"description=Autonomy overlay"      toml:"autonomy,omitzero"`
	Permissions   Permissions   `json:"permissions,omitzero"   jsonschema:"description=Permissions overlay"   toml:"permissions,omitzero"`
	Models        Models        `json:"models,omitzero"        jsonschema:"description=Models overlay"        toml:"models,omitzero"`
	Context       Context       `json:"context,omitzero"       jsonschema:"description=Context overlay"       toml:"context,omitzero"`
	UI            UI            `json:"ui,omitzero"            jsonschema:"description=UI overlay"            toml:"ui,omitzero"`
	Notifications Notifications `json:"notifications,omitzero" jsonschema:"description=Notifications overlay" toml:"notifications,omitzero"`
	Voice         Voice         `json:"voice,omitzero"         jsonschema:"description=Voice overlay"         toml:"voice,omitzero"`
	Telemetry     Telemetry     `json:"telemetry,omitzero"     jsonschema:"description=Telemetry overlay"     toml:"telemetry,omitzero"`
	LSP           LSP           `json:"lsp,omitzero"           jsonschema:"description=LSP overlay"           toml:"lsp,omitzero"`
	Plugins       Plugins       `json:"plugins,omitzero"       jsonschema:"description=Plugins overlay"       toml:"plugins,omitzero"`
	MCP           MCP           `json:"mcp,omitzero"           jsonschema:"description=MCP overlay"           toml:"mcp,omitzero"`
	Harness       HarnessTable  `json:"harness,omitzero"       jsonschema:"description=Per-harness overlay"   toml:"harness,omitzero"`
}

// Allowed values of autonomy.execution_mode and autonomy.sandbox_mode. Each
// harness maps them to its native values (docs/features.md); a value a harness
// cannot express exactly is not written there and warns.
const (
	ExecutionAsk  = "ask"
	ExecutionAuto = "auto"
	ExecutionFull = "full"

	SandboxOff       = "off"
	SandboxWorkspace = "workspace"
	SandboxReadOnly  = "read-only"
)

// ExecutionModes and SandboxModes list the allowed values in order.
func ExecutionModes() []string { return []string{ExecutionAsk, ExecutionAuto, ExecutionFull} }

// SandboxModes lists the allowed values of autonomy.sandbox_mode.
func SandboxModes() []string { return []string{SandboxOff, SandboxWorkspace, SandboxReadOnly} }

// Autonomy is how freely agents act.
type Autonomy struct {
	ExecutionMode            string `json:"execution_mode,omitempty"             jsonschema:"enum=ask,enum=auto,enum=full,description=How freely agents act"                toml:"execution_mode,omitempty"`
	SandboxMode              string `json:"sandbox_mode,omitempty"               jsonschema:"enum=off,enum=workspace,enum=read-only,description=Sandbox for agent commands" toml:"sandbox_mode,omitempty"`
	AllowNonWorkspaceAccess  bool   `json:"allow_non_workspace_access,omitempty" jsonschema:"description=Allow access outside the workspace"                                toml:"allow_non_workspace_access,omitempty"`
	SubagentDepth            int    `json:"subagent_depth,omitempty"             jsonschema:"description=Maximum subagent nesting depth"                                    toml:"subagent_depth,omitempty"`
	AllowSubagentInvocations bool   `json:"allow_subagent_invocations,omitempty" jsonschema:"description=Allow agents to invoke subagents"                                  toml:"allow_subagent_invocations,omitempty"`
	AskTimeout               string `json:"ask_timeout,omitempty"                jsonschema:"description=How long to wait for an answer to a question"                      toml:"ask_timeout,omitempty"`
}

// Permissions are the command and path rules shared by all harnesses.
type Permissions struct {
	AllowCommands        []string          `json:"allow_commands,omitempty"         jsonschema:"description=Commands allowed without asking"                                          toml:"allow_commands,omitempty"`
	AllowedURLs          []string          `json:"allowed_urls,omitempty"           jsonschema:"description=URLs agents may fetch"                                                    toml:"allowed_urls,omitempty"`
	AllowCommandPatterns []string          `json:"allow_command_patterns,omitempty" jsonschema:"description=Regular expressions of commands allowed without asking"                   toml:"allow_command_patterns,omitempty"`
	DenyRead             []string          `json:"deny_read,omitempty"              jsonschema:"description=Path globs agents may not read"                                           toml:"deny_read,omitempty"`
	DenyWrite            []string          `json:"deny_write,omitempty"             jsonschema:"description=Path globs agents may not write"                                          toml:"deny_write,omitempty"`
	DenyCommandPatterns  []string          `json:"deny_command_patterns,omitempty"  jsonschema:"description=Regular expressions of denied commands"                                   toml:"deny_command_patterns,omitempty"`
	DenyCommandExact     []string          `json:"deny_command_exact,omitempty"     jsonschema:"description=Exact command lines that are denied"                                      toml:"deny_command_exact,omitempty"`
	DenyCommandPrefixes  []string          `json:"deny_command_prefixes,omitempty"  jsonschema:"description=Command prefixes that are denied"                                         toml:"deny_command_prefixes,omitempty"`
	AskCommandPrefixes   []string          `json:"ask_command_prefixes,omitempty"   jsonschema:"description=Command prefixes that always ask"                                         toml:"ask_command_prefixes,omitempty"`
	AllowedMCP           []string          `json:"allowed_mcp,omitempty"            jsonschema:"description=MCP servers or tools allowed without asking"                              toml:"allowed_mcp,omitempty"`
	Reasons              map[string]string `json:"reasons,omitempty"                jsonschema:"description=Message shown when a permission rule matches (keyed by the rule pattern)" toml:"reasons,omitempty"`
}

// Models holds the model tiers, the agent assignments and the catalog
// preferences. Tier names and assignment keys are free strings.
type Models struct {
	DefaultEffortLevel    string            `json:"default_effort_level,omitempty"    jsonschema:"description=Default reasoning effort"                                                 toml:"default_effort_level,omitempty"`
	ShowThinkingSummaries bool              `json:"show_thinking_summaries,omitempty" jsonschema:"description=Show thinking summaries"                                                  toml:"show_thinking_summaries,omitempty"`
	Default               string            `json:"default,omitempty"                 jsonschema:"description=Tier of each harness's main session model"                                toml:"default,omitempty"`
	Tiers                 map[string]Tier   `json:"tiers,omitempty"                   jsonschema:"description=Named model tiers (any name)"                                             toml:"tiers,omitempty"`
	Assign                map[string]string `json:"assign,omitempty"                  jsonschema:"description=Tier per agent id (any key; globs allowed; an exact key wins over globs)" toml:"assign,omitempty"`
	Catalog               Catalog           `json:"catalog,omitzero"                  jsonschema:"description=Model catalog preferences"                                                toml:"catalog,omitzero"`
}

// Tier is one named tier: a native model id per harness, all optional. A
// harness without a model keeps its plugin or session default.
type Tier struct {
	Claude      string            `json:"claude,omitempty"      jsonschema:"description=Claude Code model id"                            toml:"claude,omitempty"`
	Copilot     string            `json:"copilot,omitempty"     jsonschema:"description=Copilot CLI model id"                            toml:"copilot,omitempty"`
	VSCode      string            `json:"vscode,omitempty"      jsonschema:"description=VS Code model id"                                toml:"vscode,omitempty"`
	Codex       string            `json:"codex,omitempty"       jsonschema:"description=Codex model id"                                  toml:"codex,omitempty"`
	OpenCode    string            `json:"opencode,omitempty"    jsonschema:"description=opencode model id (provider/id)"                 toml:"opencode,omitempty"`
	Pi          string            `json:"pi,omitempty"          jsonschema:"description=pi model id"                                     toml:"pi,omitempty"`
	Antigravity string            `json:"antigravity,omitempty" jsonschema:"description=Antigravity model id"                            toml:"antigravity,omitempty"`
	Effort      string            `json:"effort,omitempty"      jsonschema:"description=Reasoning effort"                                toml:"effort,omitempty"`
	Efforts     map[string]string `json:"efforts,omitempty"     jsonschema:"description=Reasoning effort per harness (overrides effort)" toml:"efforts,omitempty"`
}

// Model returns the tier's model id for h, "" when unset.
func (t Tier) Model(h Harness) string {
	return map[Harness]string{
		Claude: t.Claude, Copilot: t.Copilot, VSCode: t.VSCode, Codex: t.Codex,
		OpenCode: t.OpenCode, Pi: t.Pi, Antigravity: t.Antigravity,
	}[h]
}

// EffortFor returns the tier's effort for h: the per-harness override, else effort.
func (t Tier) EffortFor(h Harness) string {
	if e := t.Efforts[string(h)]; e != "" {
		return e
	}
	return t.Effort
}

// Catalog holds the model catalog preferences.
type Catalog struct {
	Providers []string                     `json:"providers,omitempty" jsonschema:"description=Providers to include"                     toml:"providers,omitempty"`
	Since     string                       `json:"since,omitempty"     jsonschema:"description=Ignore models released before this date"  toml:"since,omitempty"`
	Pins      map[string]string            `json:"pins,omitempty"      jsonschema:"description=Pinned model per tier or role (any name)" toml:"pins,omitempty"`
	Ignore    []string                     `json:"ignore,omitempty"    jsonschema:"description=Models to ignore"                         toml:"ignore,omitempty"`
	Aliases   map[string]map[string]string `json:"aliases,omitempty"   jsonschema:"description=Model aliases per provider (any name)"    toml:"aliases,omitempty"`
}

// Context is the context window and prompt cache behaviour.
type Context struct {
	AutoCompact            bool   `json:"auto_compact,omitempty"              jsonschema:"description=Compact the context automatically"     toml:"auto_compact,omitempty"`
	AutoCompactWindow      int    `json:"auto_compact_window,omitempty"       jsonschema:"description=Context size that triggers compaction" toml:"auto_compact_window,omitempty"`
	PromptCacheTTL         string `json:"prompt_cache_ttl,omitempty"          jsonschema:"description=Prompt cache lifetime"                 toml:"prompt_cache_ttl,omitempty"`
	SubagentPromptCacheTTL string `json:"subagent_prompt_cache_ttl,omitempty" jsonschema:"description=Prompt cache lifetime for subagents"   toml:"subagent_prompt_cache_ttl,omitempty"`
	MaxTurnTokens          int    `json:"max_turn_tokens,omitempty"           jsonschema:"description=Token limit of one turn"               toml:"max_turn_tokens,omitempty"`
	MaxTokens              int    `json:"max_tokens,omitempty"                jsonschema:"description=Token limit of one response"           toml:"max_tokens,omitempty"`
}

// UI is the terminal UI preferences.
type UI struct {
	Theme            string `json:"theme,omitempty"              jsonschema:"description=Colour theme"                 toml:"theme,omitempty"`
	TUIMode          string `json:"tui_mode,omitempty"           jsonschema:"description=Terminal UI mode"             toml:"tui_mode,omitempty"`
	ViewMode         string `json:"view_mode,omitempty"          jsonschema:"description=Transcript view mode"         toml:"view_mode,omitempty"`
	OutputStyle      string `json:"output_style,omitempty"       jsonschema:"description=Output style"                 toml:"output_style,omitempty"`
	ShowTurnDuration bool   `json:"show_turn_duration,omitempty" jsonschema:"description=Show how long each turn took" toml:"show_turn_duration,omitempty"`
	ProgressBar      bool   `json:"progress_bar,omitempty"       jsonschema:"description=Show a progress bar"          toml:"progress_bar,omitempty"`
	TimeFormat       string `json:"time_format,omitempty"        jsonschema:"description=Time format"                  toml:"time_format,omitempty"`
}

// Notifications is the notification preferences.
type Notifications struct {
	TerminalBell bool `json:"terminal_bell,omitempty" jsonschema:"description=Ring the terminal bell when a turn ends" toml:"terminal_bell,omitempty"`
}

// Voice is the voice input preferences.
type Voice struct {
	Enabled    bool   `json:"enabled,omitempty"     jsonschema:"description=Enable voice input"                  toml:"enabled,omitempty"`
	Mode       string `json:"mode,omitempty"        jsonschema:"description=Voice mode"                          toml:"mode,omitempty"`
	AutoSubmit bool   `json:"auto_submit,omitempty" jsonschema:"description=Submit the transcript automatically" toml:"auto_submit,omitempty"`
	Language   string `json:"language,omitempty"    jsonschema:"description=Dictation language"                  toml:"language,omitempty"`
}

// Telemetry is the OpenTelemetry export settings.
type Telemetry struct {
	Enabled          bool              `json:"enabled,omitempty"           jsonschema:"description=Export telemetry"                                                     toml:"enabled,omitempty"`
	Endpoint         string            `json:"endpoint,omitempty"          jsonschema:"description=Collector endpoint"                                                   toml:"endpoint,omitempty"`
	Protocol         string            `json:"protocol,omitempty"          jsonschema:"description=Export protocol"                                                      toml:"protocol,omitempty"`
	Temporality      string            `json:"temporality,omitempty"       jsonschema:"description=Metric temporality"                                                   toml:"temporality,omitempty"`
	LogToolDetails   bool              `json:"log_tool_details,omitempty"  jsonschema:"description=Log tool call details"                                                toml:"log_tool_details,omitempty"`
	LogToolContent   bool              `json:"log_tool_content,omitempty"  jsonschema:"description=Log tool call content"                                                toml:"log_tool_content,omitempty"`
	ServiceNamespace string            `json:"service_namespace,omitempty" jsonschema:"description=Service namespace resource attribute"                                 toml:"service_namespace,omitempty"`
	Attributes       map[string]string `json:"attributes,omitempty"        jsonschema:"description=Extra resource attributes (any name)"                                 toml:"attributes,omitempty"`
	Services         map[string]string `json:"services,omitempty"          jsonschema:"description=Service name per harness (any name)"                                  toml:"services,omitempty"`
	Harnesses        []Harness         `json:"harnesses,omitempty"         jsonschema:"description=Harnesses that export telemetry (unset means every selected harness)" toml:"harnesses,omitempty"`
}

// Exports reports whether telemetry is on for h: enabled, and h is listed
// when Harnesses is not empty.
func (t Telemetry) Exports(h Harness) bool {
	return t.Enabled && (len(t.Harnesses) == 0 || slices.Contains(t.Harnesses, h))
}

// LSP is the language server list.
type LSP struct {
	Enabled bool                 `json:"enabled,omitempty" jsonschema:"description=Enable language servers"     toml:"enabled,omitempty"`
	Servers map[string]LSPServer `json:"servers,omitempty" jsonschema:"description=Language servers (any name)" toml:"servers,omitempty"`
}

// LSPServer is one language server.
type LSPServer struct {
	Command    []string          `json:"command,omitempty"    jsonschema:"description=Command line that starts the server"      toml:"command,omitempty"`
	Extensions map[string]string `json:"extensions,omitempty" jsonschema:"description=File extension to language id (any name)" toml:"extensions,omitempty"`
	Filetypes  []string          `json:"filetypes,omitempty"  jsonschema:"description=Editor file types"                        toml:"filetypes,omitempty"`
	Format     bool              `json:"format,omitempty"     jsonschema:"description=Use the server to format"                 toml:"format,omitempty"`
}

// Instructions describes the instruction layers.
type Instructions struct {
	Dir         string                      `json:"dir,omitempty"          jsonschema:"description=Directory of instruction files (default is the config dir plus /instructions)" toml:"dir,omitempty"`
	EditCommand []string                    `json:"edit_command,omitempty" jsonschema:"description=Command that edits an instruction file"                                        toml:"edit_command,omitempty"`
	Vars        map[string]any              `json:"vars,omitempty"         jsonschema:"description=Template variables (any name)"                                                 toml:"vars,omitempty"`
	Layers      map[string]InstructionLayer `json:"layers,omitempty"       jsonschema:"description=Declared layers (any name)"                                                    toml:"layers,omitempty"`
}

// InstructionLayer is one declared instruction layer.
type InstructionLayer struct {
	File string `json:"file,omitempty" jsonschema:"description=File of the layer relative to the instructions dir" toml:"file,omitempty"`
}

// Plugins is the plugin registry and per-harness selection.
type Plugins struct {
	Registry   string                      `json:"registry,omitempty"   jsonschema:"description=Plugin registry repository"                             toml:"registry,omitempty"`
	Enabled    map[string][]Harness        `json:"enabled,omitempty"    jsonschema:"description=Harnesses each plugin is enabled for (any plugin name)" toml:"enabled,omitempty"`
	Disabled   []string                    `json:"disabled,omitempty"   jsonschema:"description=Plugins that stay off"                                  toml:"disabled,omitempty"`
	Standalone map[string]StandalonePlugin `json:"standalone,omitempty" jsonschema:"description=Plugins outside any registry (any name)"                toml:"standalone,omitempty"`
	External   map[string]StandalonePlugin `json:"external,omitempty"   jsonschema:"description=Alias of standalone for external plugins (any name)"    toml:"external,omitempty"`
}

// StandalonePlugin is a plugin outside any registry.
type StandalonePlugin struct {
	Source    string   `json:"source,omitempty"    jsonschema:"description=Repository or URL of the plugin" toml:"source,omitempty"`
	Path      string   `json:"path,omitempty"      jsonschema:"description=Path inside the source"          toml:"path,omitempty"`
	Harnesses []string `json:"harnesses,omitempty" jsonschema:"description=Harnesses the plugin applies to" toml:"harnesses,omitempty"`
	Version   string   `json:"version,omitempty"   jsonschema:"description=Version or ref"                  toml:"version,omitempty"`
}

// MCP holds the MCP servers.
type MCP struct {
	Servers    map[string]MCPServer `json:"servers,omitempty"    jsonschema:"description=MCP servers (any name)"                           toml:"servers,omitempty"`
	Workspaces map[string]Workspace `json:"workspaces,omitempty" jsonschema:"description=Workspaces with their own MCP servers (any name)" toml:"workspaces,omitempty"`
}

// Workspace is a directory with its own MCP servers.
type Workspace struct {
	Path    string               `json:"path,omitempty"    jsonschema:"description=Workspace directory"                     toml:"path,omitempty"`
	Servers map[string]MCPServer `json:"servers,omitempty" jsonschema:"description=MCP servers of the workspace (any name)" toml:"servers,omitempty"`
	// ExpandEnv writes the expanded ${VAR} values into the workspace files,
	// for every harness. Refused when the path is inside a git work tree.
	ExpandEnv bool `json:"expand_env,omitempty" jsonschema:"description=Write expanded ${VAR} values (secrets in plaintext) into the workspace files for every harness; refused inside a git work tree" toml:"expand_env,omitempty"`
}

// MCPServer is one MCP server: a url or a command.
type MCPServer struct {
	Type    string            `json:"type,omitempty"    jsonschema:"description=Transport type"                                 toml:"type,omitempty"`
	URL     string            `json:"url,omitempty"     jsonschema:"description=Remote server URL"                              toml:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty" jsonschema:"description=HTTP headers (any name)"                        toml:"headers,omitempty"`
	Command string            `json:"command,omitempty" jsonschema:"description=Local server command"                           toml:"command,omitempty"`
	Args    []string          `json:"args,omitempty"    jsonschema:"description=Arguments of the command"                       toml:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"     jsonschema:"description=Environment of the command (any name)"          toml:"env,omitempty"`
	OAuth   OAuth             `json:"oauth,omitzero"    jsonschema:"description=Pre-registered OAuth client (url servers only)" toml:"oauth,omitzero"`
}

// OAuth is a pre-registered OAuth client.
type OAuth struct {
	ClientID     string `json:"client_id,omitempty"     jsonschema:"description=OAuth client id"                  toml:"client_id,omitempty"`
	CallbackPort int    `json:"callback_port,omitempty" jsonschema:"description=Local callback port (1 to 65535)" toml:"callback_port,omitempty"`
	PublicClient *bool  `json:"public_client,omitempty" jsonschema:"description=The client has no secret"         toml:"public_client,omitempty"`
}

// HarnessTable has one typed table per harness.
type HarnessTable struct {
	Claude      ClaudeSettings      `json:"claude,omitzero"      jsonschema:"description=Claude Code settings" toml:"claude,omitzero"`
	Copilot     CopilotSettings     `json:"copilot,omitzero"     jsonschema:"description=Copilot CLI settings" toml:"copilot,omitzero"`
	VSCode      VSCodeSettings      `json:"vscode,omitzero"      jsonschema:"description=VS Code settings"     toml:"vscode,omitzero"`
	Codex       CodexSettings       `json:"codex,omitzero"       jsonschema:"description=Codex settings"       toml:"codex,omitzero"`
	OpenCode    OpenCodeSettings    `json:"opencode,omitzero"    jsonschema:"description=opencode settings"    toml:"opencode,omitzero"`
	Pi          PiSettings          `json:"pi,omitzero"          jsonschema:"description=pi settings"          toml:"pi,omitzero"`
	Antigravity AntigravitySettings `json:"antigravity,omitzero" jsonschema:"description=Antigravity settings" toml:"antigravity,omitzero"`
}

// InstructionLists returns the instructions list of every harness table.
func (t HarnessTable) InstructionLists() map[Harness][]string {
	return map[Harness][]string{
		Claude:      t.Claude.Instructions,
		Copilot:     t.Copilot.Instructions,
		VSCode:      t.VSCode.Instructions,
		Codex:       t.Codex.Instructions,
		OpenCode:    t.OpenCode.Instructions,
		Pi:          t.Pi.Instructions,
		Antigravity: t.Antigravity.Instructions,
	}
}

// ClaudeSettings are the Claude Code settings.
type ClaudeSettings struct {
	DefaultMode               string         `json:"default_mode,omitempty"                 jsonschema:"description=Default permission mode"                                 toml:"default_mode,omitempty"`
	SkipAutoPermissionPrompt  bool           `json:"skip_auto_permission_prompt,omitempty"  jsonschema:"description=Skip the auto mode permission prompt"                    toml:"skip_auto_permission_prompt,omitempty"`
	CleanupPeriodDays         int            `json:"cleanup_period_days,omitempty"          jsonschema:"description=Days to keep transcripts"                                toml:"cleanup_period_days,omitempty"`
	DisableClaudeAIConnectors bool           `json:"disable_claude_ai_connectors,omitempty" jsonschema:"description=Turn off claude.ai connectors"                           toml:"disable_claude_ai_connectors,omitempty"`
	Allow                     []string       `json:"allow,omitempty"                        jsonschema:"description=Extra allow rules in native syntax"                      toml:"allow,omitempty"`
	Settings                  map[string]any `json:"settings,omitempty"                     jsonschema:"description=Native settings merged as is (any key)"                  toml:"settings,omitempty"`
	Instructions              []string       `json:"instructions,omitempty"                 jsonschema:"description=Instruction layers composed for this harness (in order)" toml:"instructions,omitempty"`
}

// CopilotSettings are the Copilot CLI settings.
type CopilotSettings struct {
	Settings     map[string]any `json:"settings,omitempty"     jsonschema:"description=Native settings merged as is (any key)"                  toml:"settings,omitempty"`
	Instructions []string       `json:"instructions,omitempty" jsonschema:"description=Instruction layers composed for this harness (in order)" toml:"instructions,omitempty"`
}

// VSCodeSettings are the VS Code settings.
type VSCodeSettings struct {
	Settings     map[string]any `json:"settings,omitempty"     jsonschema:"description=Native settings merged as is (any key)"                  toml:"settings,omitempty"`
	Instructions []string       `json:"instructions,omitempty" jsonschema:"description=Instruction layers composed for this harness (in order)" toml:"instructions,omitempty"`
}

// CodexSettings are the Codex settings.
type CodexSettings struct {
	Model                string         `json:"model,omitempty"                  jsonschema:"description=Model identifier"                                        toml:"model,omitempty"`
	ModelReasoningEffort string         `json:"model_reasoning_effort,omitempty" jsonschema:"description=Reasoning effort"                                        toml:"model_reasoning_effort,omitempty"`
	ApprovalPolicy       string         `json:"approval_policy,omitempty"        jsonschema:"description=Approval policy"                                         toml:"approval_policy,omitempty"`
	SandboxMode          string         `json:"sandbox_mode,omitempty"           jsonschema:"description=Sandbox mode"                                            toml:"sandbox_mode,omitempty"`
	Settings             map[string]any `json:"settings,omitempty"               jsonschema:"description=Native settings merged as is (any key)"                  toml:"settings,omitempty"`
	Instructions         []string       `json:"instructions,omitempty"           jsonschema:"description=Instruction layers composed for this harness (in order)" toml:"instructions,omitempty"`
}

// OpenCodeSettings are the opencode settings.
type OpenCodeSettings struct {
	Settings     map[string]any `json:"settings,omitempty"     jsonschema:"description=Native settings merged as is (any key)"                  toml:"settings,omitempty"`
	Instructions []string       `json:"instructions,omitempty" jsonschema:"description=Instruction layers composed for this harness (in order)" toml:"instructions,omitempty"`
}

// PiSettings are the pi settings.
type PiSettings struct {
	DefaultProvider      string         `json:"default_provider,omitempty"       jsonschema:"description=Default provider"                                        toml:"default_provider,omitempty"`
	DefaultModel         string         `json:"default_model,omitempty"          jsonschema:"description=Default model"                                           toml:"default_model,omitempty"`
	DefaultThinkingLevel string         `json:"default_thinking_level,omitempty" jsonschema:"description=Default thinking level"                                  toml:"default_thinking_level,omitempty"`
	Packages             []string       `json:"packages,omitempty"               jsonschema:"description=pi packages to install"                                  toml:"packages,omitempty"`
	Settings             map[string]any `json:"settings,omitempty"               jsonschema:"description=Native settings merged as is (any key)"                  toml:"settings,omitempty"`
	Instructions         []string       `json:"instructions,omitempty"           jsonschema:"description=Instruction layers composed for this harness (in order)" toml:"instructions,omitempty"`
}

// AntigravitySettings are the Antigravity settings.
type AntigravitySettings struct {
	AgentMode               string         `json:"agent_mode,omitempty"                 jsonschema:"description=Agent mode"                                              toml:"agent_mode,omitempty"`
	ToolPermission          string         `json:"tool_permission,omitempty"            jsonschema:"description=Tool permission level"                                   toml:"tool_permission,omitempty"`
	EnableTerminalSandbox   *bool          `json:"enable_terminal_sandbox,omitempty"    jsonschema:"description=Sandbox the terminal"                                    toml:"enable_terminal_sandbox,omitempty"`
	AllowNonWorkspaceAccess bool           `json:"allow_non_workspace_access,omitempty" jsonschema:"description=Allow access outside the workspace"                      toml:"allow_non_workspace_access,omitempty"`
	Allow                   []string       `json:"allow,omitempty"                      jsonschema:"description=Extra allow rules in native syntax"                      toml:"allow,omitempty"`
	Settings                map[string]any `json:"settings,omitempty"                   jsonschema:"description=Native settings merged as is (any key)"                  toml:"settings,omitempty"`
	Instructions            []string       `json:"instructions,omitempty"               jsonschema:"description=Instruction layers composed for this harness (in order)" toml:"instructions,omitempty"`
}
