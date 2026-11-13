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
	out := []Feature{
		internal("schema_version", "Version of the config file format (must be 1)."),
		internal("profile", "Selected profile, a machine fact."),
		internal("harnesses", "Harnesses to configure."),
		internal("profiles", "Named overlays merged into the config before rendering."),
		internal("os", "Overlays for one operating system, merged before rendering."),
		internal("instructions.edit_command", "Command that edits an instruction layer."),
		internal("models.catalog", "Model catalog preferences for agents models; never written to a harness."),
		internal("plugins.standalone", "Plugins outside the registry, managed by agents plugins."),
		internal("plugins.external", "Plugins outside the registry, managed by agents plugins."),
	}
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
			pl(copilot, "effortLevel in settings.json (agents writes it only from a tier today)"),
			pl(codex, "model_reasoning_effort (agents writes it only from a tier today)"),
			pl(pi, "defaultThinkingLevel (agents writes it only from a tier today)")),
		shared("models.show_thinking_summaries", "Show the model's thinking summaries.",
			"no documented thinking display setting",
			rd(claude, at(claudeSettings, "showThinkingSummaries")),
			pl(copilot, "showReasoning"),
			pl(vscode, "chat.agent.thinkingStyle"),
			pl(codex, "model_reasoning_summary and hide_agent_reasoning"),
			pl(pi, "hideThinkingBlock")),
	}
}

func autonomyFeatures() []Feature {
	const modeAmb = "ambiguous: native mode names differ and the value is a free name"
	allAmbiguous := make([]pair, 0, len(HarnessIDs()))
	for _, h := range HarnessIDs() {
		allAmbiguous = append(allAmbiguous, pl(h, "ambiguous: no renderer reads autonomy.mode and its meaning overlaps autonomy.execution_mode"))
	}
	return []Feature{
		shared("autonomy.execution_mode", "How freely agents act.",
			"no documented permission mode setting",
			rd(antigravity, at(antigravitySettings, "agentMode")),
			pl(claude, "permissions.defaultMode (agents writes it from harness.claude.default_mode; "+modeAmb+")"),
			pl(copilot, "defaultPermissionMode ("+modeAmb+")"),
			pl(vscode, "chat.permissions.default ("+modeAmb+")"),
			pl(codex, "approval_policy (agents writes it from harness.codex.approval_policy; "+modeAmb+")")),
		shared("autonomy.sandbox_mode", "Sandbox mode name.",
			"the harness has no sandbox",
			pl(claude, "sandbox.enabled (a boolean; "+modeAmb+")"),
			pl(copilot, "sandbox.enabled (a boolean; "+modeAmb+")"),
			pl(vscode, "chat.agent.sandbox.enabled (a boolean; "+modeAmb+")"),
			pl(codex, "sandbox_mode (agents writes it from harness.codex.sandbox_mode)"),
			pl(antigravity, "enableTerminalSandbox (a boolean; "+modeAmb+")")),
		shared("autonomy.allow_non_workspace_access", "Allow access outside the workspace.",
			"no documented setting for access outside the workspace",
			rd(antigravity, at(antigravitySettings, "allowNonWorkspaceAccess")),
			pl(claude, "additionalDirectories (ambiguous: it takes a directory list, not a boolean)"),
			pl(vscode, "github.copilot.chat.additionalReadAccessFolders (ambiguous: it takes a folder list, not a boolean)"),
			pl(opencode, "permission external_directory")),
		shared("autonomy.subagent_depth", "Maximum subagent nesting depth.",
			"no documented subagent depth setting",
			rd(opencode, at(opencodeConfig, "experimental")),
			pl(copilot, "subagents.maxDepth"),
			un(codex, "agents.max_threads limits concurrent threads, not nesting depth")),
		shared("autonomy.allow_subagent_invocations", "Allow subagents to invoke subagents.",
			"no documented subagent invocation setting",
			pl(vscode, "chat.subagents.allowInvocationsFromSubagents")),
		shared("autonomy.ask_timeout", "How long to wait for an answer to a question.",
			"no documented timeout for questions",
			rd(claude, at(claudeSettings, "askUserQuestionTimeout"))),
		shared("autonomy.mode", "Default agent mode.", "", allAmbiguous...),
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
			pl(claude, "permissions.allow Bash(...) rules"),
			pl(codex, "prefix_rule with decision allow"),
			pl(opencode, "permission bash allow"),
			pl(antigravity, "permissions.allow command(...)")),
		shared("permissions.allowed_urls", "URLs agents may fetch.", "no documented URL allow list",
			rd(copilot, at(copilotSettings, "allowedUrls")),
			rd(vscode, all(at(vscodeSettings, "chat.tools.urls.autoApprove"), at(copilotSettings, "allowedUrls"))),
			pl(claude, "permissions.allow WebFetch(domain:...) rules"),
			pl(opencode, "permission webfetch"),
			pl(antigravity, "permissions.allow read_url(...)")),
		shared("permissions.allow_command_patterns", "Command patterns allowed without asking.", noCmd,
			claudeAllow, vscodeCommands, codexRule, ocAllow, agyAllow),
		shared("permissions.deny_read", "Path globs agents may not read.", noPath,
			claudeDeny, ocDeny, agyDeny),
		shared("permissions.deny_write", "Path globs agents may not write.", noPath,
			claudeDeny, ocDeny, agyDeny),
		shared("permissions.deny_command_patterns", "Command patterns that are denied.", noCmd,
			claudeDeny, vscodeCommands, codexRule, ocDeny, agyDeny),
		shared("permissions.deny_command_exact", "Exact command lines that are denied.", noCmd,
			claudeDeny, vscodeCommands, ocDeny, agyDeny,
			pl(codex, "prefix_rule with decision forbidden")),
		shared("permissions.deny_command_prefixes", "Command prefixes that are denied.", noCmd,
			claudeDeny, ocDeny, agyDeny,
			pl(vscode, "chat.tools.terminal.autoApprove entries set to false"),
			pl(codex, "prefix_rule with decision forbidden")),
		shared("permissions.ask_command_prefixes", "Command prefixes that always ask.", noCmd,
			claudeAsk, vscodeCommands, ocAllow, agyAsk,
			pl(codex, "prefix_rule with decision prompt")),
		shared("permissions.allowed_mcp", "MCP servers or tools allowed without asking.",
			"no documented MCP approval setting",
			pl(claude, "permissions.allow mcp__<server> entries (allowedMcpServers is not an approval list)"),
			pl(opencode, "permission actions for MCP tools"),
			pl(antigravity, "permissions.allow mcp(...)")),
		shared("permissions.reasons", "Message shown when a permission rule matches, keyed by the rule pattern.",
			"no documented per-rule message",
			pl(codex, "prefix_rule justification")),
	}
}

func contextFeatures() []Feature {
	return []Feature{
		shared("context.auto_compact", "Compact the context automatically.",
			"no documented automatic compaction setting",
			pl(claude, "autoCompactEnabled"),
			pl(vscode, "github.copilot.chat.summarizeAgentConversationHistory.enabled"),
			pl(opencode, "compaction.auto"),
			pl(pi, "compaction.enabled")),
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
			pl(claude, "env CLAUDE_CODE_MAX_OUTPUT_TOKENS")),
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
			pl(copilot, "theme"),
			pl(codex, "tui.theme"),
			pl(pi, "theme"),
			pl(antigravity, "colorScheme"),
			pl(opencode, "ambiguous: theme is not among the researched opencode keys; verify against its config docs")),
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
			pl(antigravity, "notifications")),
	}
}

func voiceFeatures() []Feature {
	const noVoice = "no documented voice input setting"
	const copilotAmb = "ambiguous: the Copilot CLI documents no voice key; agents writes voice.enabled only"
	return []Feature{
		shared("voice.enabled", "Enable voice input.", noVoice,
			rd(claude, at(claudeSettings, "voice")),
			rd(copilot, at(copilotSettings, "voice")),
			rd(vscode, all(at(vscodeSettings, "dictation.enabled"), at(copilotSettings, "voice")))),
		shared("voice.mode", "Voice mode.", noVoice,
			rd(claude, at(claudeSettings, "voice")),
			pl(copilot, copilotAmb)),
		shared("voice.auto_submit", "Submit the transcript automatically.", noVoice,
			rd(claude, at(claudeSettings, "voice")),
			pl(copilot, copilotAmb)),
		shared("voice.language", "Dictation language.", noVoice,
			rd(claude, at(claudeSettings, "voice")),
			rd(vscode, at(vscodeSettings, "agents.voice.language")),
			pl(copilot, copilotAmb)),
	}
}

func telemetryFeatures() []Feature {
	const noOTel = "no documented OpenTelemetry export setting"
	const codexAmb = "ambiguous: the [otel] sub-keys for this setting were not verified"
	type spec struct {
		key, summary string
		codex        pair
	}
	otel := rd(codex, at(codexConfig, "otel"))
	specs := []spec{
		{"enabled", "Export telemetry.", otel},
		{"endpoint", "Collector endpoint.", otel},
		{"protocol", "Export protocol.", otel},
		{"temporality", "Metric temporality.", pl(codex, codexAmb)},
		{"log_tool_details", "Log tool call details.", pl(codex, codexAmb)},
		{"log_tool_content", "Log tool call content.", pl(codex, codexAmb)},
		{"service_namespace", "Service namespace resource attribute.", pl(codex, codexAmb)},
		{"attributes", "Extra resource attributes.", otel},
		{"services", "Service name per harness.", pl(codex, codexAmb)},
	}
	out := make([]Feature, len(specs))
	for i, s := range specs {
		out[i] = shared("telemetry."+s.key, s.summary, noOTel,
			rd(claude, at(claudeSettings, "env")),
			s.codex,
			pl(vscode, "github.copilot.chat.otel.*"))
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
			pl(opencode, "formatter key (separate from lsp)")),
	}
}

func pluginFeatures() []Feature {
	const noPlugins = "no documented plugin setting"
	plugins := []pair{
		rd(claude, at(claudeSettings, "enabledPlugins", "extraKnownMarketplaces")),
		rd(copilot, at(copilotSettings, "enabledPlugins", "extraKnownMarketplaces")),
		rd(vscode, at(copilotSettings, "enabledPlugins", "extraKnownMarketplaces")),
		rd(pi, at(piSettings, "packages")),
		pl(codex, "plugins.<name> enabled (agents plugins installs them instead)"),
		pl(opencode, "plugin array (agents plugins installs them instead)"),
	}
	return []Feature{
		shared("plugins.registry", "Plugin registry repository.", noPlugins, plugins...),
		shared("plugins.enabled", "Harnesses each plugin is enabled for.", noPlugins, plugins...),
		shared("plugins.disabled", "Plugins that stay off.", noPlugins, plugins...),
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
		if p.h != opencode && p.h != antigravity {
			withoutPort = append(withoutPort, p)
		}
	}
	withoutPort = append(withoutPort,
		un(opencode, "no callback port field for an OAuth client"),
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
	out = append(out,
		own("harness.opencode.default_model", "Default model.",
			pl(opencode, "model (ambiguous: the key is accepted and suppresses the tier model, but no renderer writes it)")),
		own("harness.opencode.sandbox", "Run sandboxed.", un(opencode, "no sandbox setting")),
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
