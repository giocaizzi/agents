package feature

import "slices"

// Features is the table: one row per configuration key (or key prefix), in the
// order the matrix shows them. Cells are seeded from what the renderers in
// internal/harness write today; the rest comes from the documentation named by
// Source, checked in Checked.
var Features = slices.Concat( //nolint:gochecknoglobals // the feature table is the registry
	internalFeatures(),
	instructionFeatures(),
	modelFeatures(),
	autonomyFeatures(),
	permissionFeatures(),
	contextFeatures(),
	uiFeatures(),
	voiceFeatures(),
	telemetryFeatures(),
	lspFeatures(),
	pluginFeatures(),
	mcpFeatures(),
	harnessFeatures(),
)

func internalFeatures() []Feature {
	out := make([]Feature, 0, 9+len(HarnessIDs()))
	out = append(out,
		internal("schema_version", "Version of the config file format (must be 1)."),
		internal("profile", "Selected profile, a machine fact."),
		internal("harnesses", "Harnesses to configure."),
		internal("profiles", "Named overlays merged into the config before rendering."),
		internal("os", "Overlays for one operating system, merged before rendering."),
		internal("instructions.edit_command", "Command that edits an instruction layer."),
		internal("models.catalog", "Model catalog preferences for agents models; never written to a harness."),
		internal("plugins.standalone", "Plugins outside the registry, managed by agents plugins."),
		internal("plugins.external", "Plugins outside the registry, managed by agents plugins."),
	)
	for _, h := range HarnessIDs() {
		out = append(out, internal("harness."+h+".instructions", "Instruction layers composed for this harness."))
	}
	return out
}

func instructionFeatures() []Feature {
	ps := make([]pair, 0, len(HarnessIDs()))
	for _, h := range HarnessIDs() {
		ps = append(ps, rd(h, at(instructionFile[h])))
	}
	return []Feature{shared("instructions", "Layers composed into the global instruction file of the harness.", "", ps...)}
}

func modelFeatures() []Feature {
	session := []pair{
		rd(claude, at(claudeSettings, "model", "effortLevel")),
		rd(copilot, at(copilotSettings, "model")),
		rd(vscode, at(copilotSettings, "model")),
		rd(codex, at(codexConfig, "model", "model_reasoning_effort")),
		rd(opencode, at(opencodeConfig, "model")),
		rd(pi, at(piSettings, "defaultModel", "defaultThinkingLevel")),
	}
	const noModel = "the CLI settings list has no model setting"
	return []Feature{
		shared("models.default", "Tier of the main session model.", noModel, session...),
		shared("models.tiers", "Named tiers: a native model id and reasoning effort per harness.", noModel, session...),
		shared("models.assign", "Tier of each plugin agent, by agent id or glob.",
			"no settings-level per-agent model; the plugin's own model applies",
			rd(copilot, at(copilotSettings, "subagents")),
			rd(vscode, at(copilotSettings, "subagents")),
			rd(opencode, at(opencodeConfig, "agents")),
			un(claude, "no per-subagent model key in settings (only a global subagent model variable)")),
		shared("models.default_effort_level", "Default reasoning effort of the main session.",
			"no documented reasoning effort setting",
			rd(claude, at(claudeSettings, "effortLevel")),
			rd(copilot, at(copilotSettings, "effortLevel")),
			rd(vscode, at(copilotSettings, "effortLevel")),
			rd(codex, at(codexConfig, "model_reasoning_effort")),
			rd(pi, at(piSettings, "defaultThinkingLevel"))),
		shared("models.show_thinking_summaries", "Show the model's thinking summaries.",
			"no documented thinking display setting",
			rd(claude, at(claudeSettings, "showThinkingSummaries")),
			rd(copilot, at(copilotSettings, "showReasoning")),
			rd(vscode, at(copilotSettings, "showReasoning")),
			pl(codex, "ambiguous: a boolean maps to model_reasoning_summary (auto, concise, detailed, none) or hide_agent_reasoning (boolean); true cannot pick between auto and detailed, and false is the default"),
			rd(pi, at(piSettings, "hideThinkingBlock"))),
	}
}

func autonomyFeatures() []Feature {
	const modeAmb = "ambiguous: native mode names differ and the value is a free name"
	return []Feature{
		shared("autonomy.execution_mode", "How freely agents act.",
			"no documented permission mode setting",
			rd(antigravity, at(antigravitySettings, "agentMode")),
			pl(claude, "ambiguous: permissions.defaultMode takes one of default, acceptEdits, plan, auto, dontAsk, bypassPermissions; the shared value is a free name, so the options are passing Claude names through or a mapping table (agents writes it from harness.claude.default_mode)"),
			pl(copilot, "defaultPermissionMode ("+modeAmb+")"),
			pl(vscode, "chat.permissions.default ("+modeAmb+")"),
			pl(codex, "approval_policy takes on-request, never or a granular table, and agents writes it from harness.codex.approval_policy; "+modeAmb+"; a shared name such as auto would be an invalid Codex value")),
		shared("autonomy.sandbox_mode", "Sandbox mode name.",
			"the harness has no sandbox",
			pl(claude, "ambiguous: sandbox.enabled is a boolean but the shared value is a free mode name; the options are on for any non-empty name or a list of names that mean on"),
			pl(copilot, "sandbox.enabled (a boolean; "+modeAmb+")"),
			pl(vscode, "chat.agent.sandbox.enabled (a boolean; "+modeAmb+")"),
			pl(codex, "ambiguous: sandbox_mode takes read-only, workspace-write or danger-full-access, and agents writes it from harness.codex.sandbox_mode; the shared name is free-form, so an unrelated name would be an invalid Codex value"),
			pl(antigravity, "enableTerminalSandbox (a boolean; "+modeAmb+"; the typed harness.antigravity.enable_terminal_sandbox is always written and wins)")),
		shared("autonomy.allow_non_workspace_access", "Allow access outside the workspace.",
			"no documented setting for access outside the workspace",
			rd(antigravity, at(antigravitySettings, "allowNonWorkspaceAccess")),
			pl(claude, "ambiguous: permissions.additionalDirectories takes a directory list but the shared value is a boolean; the options are a new list key or no mapping"),
			pl(vscode, "github.copilot.chat.additionalReadAccessFolders (ambiguous: it takes a folder list, not a boolean)"),
			rd(opencode, at(opencodeConfig, "permissions"))),
		shared("autonomy.subagent_depth", "Maximum subagent nesting depth.",
			"no documented subagent depth setting",
			rd(opencode, at(opencodeConfig, "experimental")),
			rd(copilot, at(copilotSettings, "subagents")),
			rd(vscode, at(copilotSettings, "subagents")),
			un(codex, "agents.max_threads limits concurrent threads, not nesting depth")),
		shared("autonomy.allow_subagent_invocations", "Allow subagents to invoke subagents.",
			"no documented subagent invocation setting",
			rd(vscode, at(vscodeSettings, "chat.subagents.allowInvocationsFromSubagents"))),
		shared("autonomy.ask_timeout", "How long to wait for an answer to a question.",
			"no documented timeout for questions",
			rd(claude, at(claudeSettings, "askUserQuestionTimeout"))),
	}
}

func permissionFeatures() []Feature {
	claudeAllow := rd(claude, at(claudeSettings, "permissions.allow"))
	claudeDeny := rd(claude, at(claudeSettings, "permissions.deny"))
	claudeAsk := rd(claude, at(claudeSettings, "permissions.ask"))
	vscodeCommands := rd(vscode, at(vscodeSettings, "chat.tools.terminal.autoApprove"))
	codexRule := rd(codex, at(codexRules))
	ocAllow := rd(opencode, at(opencodeConfig, "permissions"))
	ocDeny := rd(opencode, at(opencodeConfig, "policies"))
	agyAllow := rd(antigravity, at(antigravitySettings, "permissions.allow"))
	agyDeny := rd(antigravity, at(antigravitySettings, "permissions.deny"))
	agyAsk := rd(antigravity, at(antigravitySettings, "permissions.ask"))
	const noCmd = "no documented command allow or deny list"
	const noPath = "no documented path deny rules"
	return []Feature{
		shared("permissions.allow_commands", "Commands allowed without asking.", noCmd,
			vscodeCommands,
			rd(claude, at(claudeSettings, "permissions.allow")),
			rd(codex, at(codexRules)),
			rd(opencode, at(opencodeConfig, "permissions")),
			agyAllow),
		shared("permissions.allowed_urls", "URLs agents may fetch.", "no documented URL allow list",
			rd(copilot, at(copilotSettings, "allowedUrls")),
			rd(vscode, all(at(vscodeSettings, "chat.tools.urls.autoApprove"), at(copilotSettings, "allowedUrls"))),
			rd(claude, at(claudeSettings, "permissions.allow")),
			rd(opencode, at(opencodeConfig, "permissions")),
			agyAllow),
		shared("permissions.allow_command_patterns", "Command patterns allowed without asking.", noCmd,
			claudeAllow, vscodeCommands, codexRule, ocAllow, agyAllow),
		shared("permissions.deny_read", "Path globs agents may not read.", noPath,
			claudeDeny, ocDeny, agyDeny),
		shared("permissions.deny_write", "Path globs agents may not write.", noPath,
			claudeDeny, ocDeny, agyDeny),
		shared("permissions.deny_command_patterns", "Command patterns that are denied.", noCmd,
			claudeDeny, vscodeCommands, codexRule, ocDeny, agyDeny),
		shared("permissions.deny_command_exact", "Exact command lines that are denied.", noCmd,
			claudeDeny, vscodeCommands, ocDeny, agyDeny, codexRule),
		shared("permissions.deny_command_prefixes", "Command prefixes that are denied.", noCmd,
			claudeDeny, ocDeny, agyDeny,
			vscodeCommands,
			codexRule),
		shared("permissions.ask_command_prefixes", "Command prefixes that always ask.", noCmd,
			claudeAsk, vscodeCommands, ocAllow, agyAsk, codexRule),
		shared("permissions.allowed_mcp", "MCP servers or tools allowed without asking.",
			"no documented MCP approval setting",
			rd(claude, at(claudeSettings, "permissions.allow")),
			pl(opencode, "ambiguous: permissions rules name an MCP tool by action <server>_<tool> (wildcards allowed), but allowed_mcp does not say whether an entry is a server or a tool, nor how a tool is spelled; options: treat each entry as a server and write <server>_*, or define a tool syntax first (https://opencode.ai/docs/permissions)"),
			agyAllow),
		shared("permissions.reasons", "Message shown when a permission rule matches, keyed by the rule pattern.",
			"no documented per-rule message",
			rd(codex, at(codexRules))),
	}
}

func contextFeatures() []Feature {
	return []Feature{
		shared("context.auto_compact", "Compact the context automatically.",
			"no documented automatic compaction setting",
			rd(claude, at(claudeSettings, "autoCompactEnabled")),
			rd(vscode, at(vscodeSettings, "github.copilot.chat.summarizeAgentConversationHistory.enabled")),
			rd(opencode, at(opencodeConfig, "compaction")),
			rd(pi, at(piSettings, "compaction.enabled"))),
		shared("context.auto_compact_window", "Context size that triggers compaction.",
			"no documented compaction trigger size",
			rd(claude, at(claudeSettings, "autoCompactWindow")),
			pl(pi, "compaction.reserveTokens (ambiguous: it reserves tokens instead of setting a trigger size)")),
		shared("context.prompt_cache_ttl", "Prompt cache lifetime.",
			"no documented prompt cache lifetime setting",
			rd(claude, at(claudeSettings, "promptCacheTtl"))),
		shared("context.subagent_prompt_cache_ttl", "Prompt cache lifetime for subagents.",
			"no documented prompt cache lifetime setting",
			rd(claude, at(claudeSettings, "subagentPromptCacheTtl"))),
		shared("context.max_turn_tokens", "Token limit of one turn.", "no documented per-turn token limit"),
		shared("context.max_tokens", "Token limit of one response.", "no documented response token limit",
			rd(claude, at(claudeSettings, "env"))),
	}
}

func uiFeatures() []Feature {
	claudeOnly := func(key, summary, native string) Feature {
		return shared("ui."+key, summary, "no documented setting for "+summary, rd(claude, at(claudeSettings, native)))
	}
	return []Feature{
		shared("ui.theme", "Colour theme.", "no documented theme setting",
			rd(claude, at(claudeSettings, "theme")),
			rd(vscode, at(vscodeSettings, "workbench.colorTheme")),
			pl(copilot, "theme (ambiguous: the CLI accepts only default, github, dim, high-contrast or colorblind, while ui.theme is a free name that VS Code reads as a colour theme)"),
			pl(codex, "ambiguous: tui.theme is a syntax-highlighting theme in kebab-case, not a colour theme, so a shared name such as dark would be an invalid Codex value"),
			rd(pi, at(piSettings, "theme")),
			rd(antigravity, at(antigravitySettings, "colorScheme")),
			pl(opencode, "ambiguous: opencode 2 keeps the theme in cli.json as theme.name (a theme such as catppuccin) plus theme.mode (system, dark or light); the shared value is a free string that is a mode for Claude Code and a theme name for VS Code, and agents writes no cli.json; options: write it as theme.name, write dark or light as theme.mode and anything else as theme.name (https://opencode.ai/docs/themes)")),
		claudeOnly("tui_mode", "Terminal UI mode.", "tui"),
		claudeOnly("view_mode", "Transcript view mode.", "viewMode"),
		claudeOnly("output_style", "Output style.", "outputStyle"),
		claudeOnly("show_turn_duration", "Show how long each turn took.", "showTurnDuration"),
		claudeOnly("progress_bar", "Show a terminal progress bar.", "terminalProgressBarEnabled"),
		claudeOnly("time_format", "Time format.", "timeFormat"),
		shared("notifications.terminal_bell", "Ring the terminal bell when a turn ends.",
			"no documented terminal bell setting",
			rd(claude, at(claudeSettings, "preferredNotifChannel")),
			rd(copilot, at(copilotSettings, "beep")),
			rd(vscode, at(copilotSettings, "beep")),
			rd(codex, at(codexConfig, "tui")),
			rd(antigravity, at(antigravitySettings, "notifications"))),
	}
}

func voiceFeatures() []Feature {
	const noVoice = "no documented voice input setting"
	const voiceUn = "the settings reference lists no voice key (checked 2026-11-13, docs.github.com Copilot CLI config directory reference)"
	return []Feature{
		shared("voice.enabled", "Enable voice input.", noVoice,
			rd(claude, at(claudeSettings, "voice")),
			rd(copilot, at(copilotSettings, "voice")),
			rd(vscode, all(at(vscodeSettings, "dictation.enabled"), at(copilotSettings, "voice")))),
		shared("voice.mode", "Voice mode.", noVoice,
			rd(claude, at(claudeSettings, "voice")),
			un(copilot, voiceUn)),
		shared("voice.auto_submit", "Submit the transcript automatically.", noVoice,
			rd(claude, at(claudeSettings, "voice")),
			un(copilot, voiceUn)),
		shared("voice.language", "Dictation language.", noVoice,
			rd(claude, at(claudeSettings, "voice")),
			rd(vscode, at(vscodeSettings, "agents.voice.language")),
			un(copilot, voiceUn)),
	}
}

func telemetryFeatures() []Feature {
	const noOTel = "no documented OpenTelemetry export setting"
	const codexNoKey = "[otel] has no such key (https://developers.openai.com/codex/config-reference, checked 2026-11-13)"
	type spec struct {
		key, summary  string
		codex, vscode pair
	}
	otel := rd(codex, at(codexConfig, "otel"))
	vsOTel := func(key string) pair {
		return rd(vscode, at(vscodeSettings, "github.copilot.chat.otel."+key))
	}
	vsNone := un(vscode, "the Copilot settings reference lists no such OpenTelemetry setting (checked 2026-11-13)")
	specs := []spec{
		{"enabled", "Export telemetry.", otel, vsOTel("enabled")},
		{"endpoint", "Collector endpoint.", otel, vsOTel("otlpEndpoint")},
		{"protocol", "Export protocol.", otel, pl(vscode, "github.copilot.chat.otel.exporterType (ambiguous: it takes otlp-http, otlp-grpc, console or file, while telemetry.protocol is a free name)")},
		{"temporality", "Metric temporality.", un(codex, codexNoKey), vsNone},
		{"log_tool_details", "Log tool call details.", un(codex, codexNoKey), vsNone},
		{"log_tool_content", "Log tool call content.", pl(codex, "ambiguous: otel.tool_result.max_bytes limits the logged tool result in bytes and there is no boolean; the options are to map true to the default limit or to leave it unsupported"), pl(vscode, "github.copilot.chat.otel.captureContent (ambiguous: it captures full prompt and response content, not only tool call content)")},
		{"service_namespace", "Service namespace resource attribute.", un(codex, "[otel] has no resource attribute keys, only otel.span_attributes for spans and otel.environment (https://developers.openai.com/codex/config-reference, checked 2026-11-13)"), vsNone},
		{"attributes", "Extra resource attributes.", otel, vsNone},
		{"services", "Service name per harness.", un(codex, "[otel] has no service name key (https://developers.openai.com/codex/config-reference, checked 2026-11-13)"), vsNone},
	}
	out := make([]Feature, len(specs))
	for i, s := range specs {
		out[i] = shared("telemetry."+s.key, s.summary, noOTel,
			rd(claude, at(claudeSettings, "env")),
			s.codex,
			s.vscode)
	}
	return out
}

func lspFeatures() []Feature {
	const noLSP = "no documented language server setting"
	servers := []pair{
		rd(claude, all(
			at(claudeSettings, "enabledPlugins", "extraKnownMarketplaces"),
			claudeMarketMD, claudePluginJSON, claudeLSPJSON)),
		rd(copilot, at(copilotLSP, "lspServers")),
		rd(vscode, at(copilotLSP, "lspServers")),
		rd(opencode, at(opencodeConfig, "lsp")),
	}
	const noExt = "language server entries have no such field"
	return []Feature{
		shared("lsp.enabled", "Enable language servers.", noLSP, servers...),
		shared("lsp.servers", "Language servers: command and file extensions.", noLSP, servers...),
		shared("lsp.servers.*.filetypes", "Editor file types of a language server.", noExt),
		shared("lsp.servers.*.format", "Use the language server to format.", noExt,
			un(opencode, "the formatter section runs a command with $FILE over file extensions and is separate from lsp, so a language server cannot be asked to format (https://opencode.ai/docs/formatters)")),
	}
}

func pluginFeatures() []Feature {
	const noPlugins = "no documented plugin setting"
	const noOpenCode = "plugins entries are npm, git or directory plugins by their own id, with no registry of harness plugins and no enabled map to fill; agents plugins installs harness plugins instead (https://opencode.ai/docs/plugins)"
	plugins := []pair{
		rd(claude, at(claudeSettings, "enabledPlugins", "extraKnownMarketplaces")),
		rd(copilot, at(copilotSettings, "enabledPlugins", "extraKnownMarketplaces")),
		rd(vscode, at(copilotSettings, "enabledPlugins", "extraKnownMarketplaces")),
		rd(pi, at(piSettings, "packages")),
		un(opencode, noOpenCode),
	}
	codexPlugins := rd(codex, at(codexConfig, "plugins"))
	return []Feature{
		shared("plugins.registry", "Plugin registry repository.", noPlugins,
			append(slices.Clone(plugins), pl(codex, "ambiguous: marketplaces.<name>.source takes a git location and agents plugins registers the marketplace through the installer; the config.toml value format for the owner/repo registry is not documented (https://developers.openai.com/codex/config-reference, checked 2026-11-13)"))...),
		shared("plugins.enabled", "Harnesses each plugin is enabled for.", noPlugins, append(slices.Clone(plugins), codexPlugins)...),
		shared("plugins.disabled", "Plugins that stay off.", noPlugins, append(slices.Clone(plugins), codexPlugins)...),
	}
}

func mcpFeatures() []Feature {
	const noMCP = "no documented MCP server setting"
	servers := []pair{
		rd(claude, at(claudeUser, mcpServer)),
		rd(copilot, at(copilotMCP, mcpServer)),
		rd(vscode, at(copilotMCP, mcpServer)),
		rd(codex, at(codexConfig, "mcp_servers")),
		rd(opencode, at(opencodeConfig, "mcp")),
		rd(pi, at(piMCP, mcpServer)),
		rd(antigravity, at(antigravityMCP, mcpServer)),
	}
	withoutPort := make([]pair, 0, len(servers))
	for _, p := range servers {
		if p.h != antigravity {
			withoutPort = append(withoutPort, p)
		}
	}
	withoutPort = append(withoutPort,
		un(antigravity, "no callback port field for an OAuth client"))
	return []Feature{
		shared("mcp.servers", "MCP servers written to the user config of each harness.", noMCP, servers...),
		shared("mcp.servers.*.oauth.callback_port", "Local OAuth callback port of an MCP server.", noMCP, withoutPort...),
		shared("mcp.servers.*.oauth.public_client", "The OAuth client of an MCP server has no secret.",
			"no public client flag in the documented MCP server fields",
			rd(copilot, at(copilotMCP, mcpServer)),
			rd(vscode, at(copilotMCP, mcpServer))),
		shared("mcp.workspaces", "MCP servers written under a workspace directory.", noMCP,
			rd(claude, at("<workspace>/.mcp.json", mcpServer)),
			rd(copilot, at("<workspace>/.mcp.json", mcpServer)),
			rd(vscode, at("<workspace>/.mcp.json", mcpServer)),
			rd(codex, all(at("<workspace>/.codex/config.toml", "mcp_servers"), at(codexConfig, "projects"))),
			rd(opencode, at("<workspace>/opencode.json", "mcp")),
			rd(pi, at("<workspace>/.pi/mcp.json", mcpServer)),
			rd(antigravity, at("<workspace>/.agents/mcp_config.json", mcpServer))),
	}
}

func harnessFeatures() []Feature {
	var out []Feature
	for _, h := range HarnessIDs() {
		out = append(out, own("harness."+h+".settings", "Native settings merged as written.",
			rd(h, at(settingsFile[h], anyKey))))
	}
	c := func(key, summary, native string) Feature {
		return own("harness.claude."+key, summary, rd(claude, at(claudeSettings, native)))
	}
	out = append(out,
		c("default_mode", "Default permission mode.", "permissions.defaultMode"),
		c("skip_auto_permission_prompt", "Skip the auto mode permission prompt.", "skipAutoPermissionPrompt"),
		c("cleanup_period_days", "Days to keep transcripts.", "cleanupPeriodDays"),
		c("disable_claude_ai_connectors", "Turn off claude.ai connectors.", "disableClaudeAiConnectors"),
		c("allow", "Extra allow rules in native syntax.", "permissions.allow"),
	)
	x := func(key, summary, native string) Feature {
		return own("harness.codex."+key, summary, rd(codex, at(codexConfig, native)))
	}
	out = append(out,
		x("model", "Model identifier.", "model"),
		x("model_reasoning_effort", "Reasoning effort.", "model_reasoning_effort"),
		x("approval_policy", "Approval policy.", "approval_policy"),
		x("sandbox_mode", "Sandbox mode.", "sandbox_mode"),
	)
	i := func(key, summary, native string) Feature {
		return own("harness.pi."+key, summary, rd(pi, at(piSettings, native)))
	}
	out = append(out,
		i("default_provider", "Default provider.", "defaultProvider"),
		i("default_model", "Default model.", "defaultModel"),
		i("default_thinking_level", "Default thinking level.", "defaultThinkingLevel"),
		i("packages", "pi packages to install.", "packages"),
	)
	a := func(key, summary, native string) Feature {
		return own("harness.antigravity."+key, summary, rd(antigravity, at(antigravitySettings, native)))
	}
	out = append(out,
		a("agent_mode", "Agent mode.", "agentMode"),
		a("tool_permission", "Tool permission level.", "toolPermission"),
		a("enable_terminal_sandbox", "Sandbox the terminal.", "enableTerminalSandbox"),
		a("allow_non_workspace_access", "Allow access outside the workspace.", "allowNonWorkspaceAccess"),
		a("allow", "Extra allow rules in native syntax.", "permissions.allow"),
	)
	return out
}
