# Harness parity matrix

What `agents` does for each policy dimension on each harness. Derived from the code, not from other docs.

## Reading the matrix

| Verb | Meaning |
|---|---|
| **renders** | `agents render` / `internal/translate` emits it. The native key follows. |
| **checks** | `agents check` verifies it. The cell says what is compared. |
| **syncs** | `agents` writes or merges the file itself (only `agents mcp sync` and `agents mcp add` do). |
| **reads** | `agents` reads harness state only (installed plugin versions, credentials, instruction files). |
| **—** | Not covered. `n/a: ...` means the harness has no such feature. `GAP: ...` means the harness supports it and `agents` does not cover it. `unknown: ...` means the evidence needed to tell the two apart is missing. |

Points that hold for the whole page:

- `agents` does not write harness settings. `agents render` prints native JSON/TOML to stdout; a config manager such as chezmoi writes the files from templates, and `agents check` inspects those templates (`CheckSourceFiles`). The only files `agents` writes itself are the MCP targets (see Instructions and MCP).
- **checks** has two depths. `parity.CheckParity` runs the translators on the policy and tests the result against the policy (see Sources, Configuration); it never opens an installed harness file. Only `parity.CheckSourceFiles` reads files from the chezmoi source directory, and only when that directory is found.
- A Policy field that no translator reads is parsed and validated (`Policy.Validate`) but not rendered. Such cells say `GAP: schema-only` when the harness has a matching native key.
- Columns follow the harness names used in code: `claude`, `copilot`, `vscode`, `codex`, `opencode`, `pi`, `antigravity`. `agents render` accepts every one except `vscode` (`internal/cli/render.go: renderHarness`); `VSCodeSettings` is only reachable from Go.
- Native-key evidence: `copilot help config` and `copilot help permissions`; `opencode.ai/config.json`; the Codex `config.schema.json` (upstream `main`); the Claude Code settings schema (schemastore); `pi --help` and the docs shipped with the pi package; `agy --help`; the translator code. Where none of these covered a harness key, the cell says `unknown`.

## Configuration

| Dimension (Policy field) | Claude Code | Copilot CLI | VS Code Copilot | Codex | opencode | pi | Antigravity |
|---|---|---|---|---|---|---|---|
| Execution mode (`autonomy.executionMode`, `autonomy.mode`, `native.claude.defaultMode`, `native.antigravity.agentMode`/`toolPermission`) | renders `permissions.defaultMode` (default `auto`); checks enum in `Validate` | — GAP: `defaultMode`, `defaultPermissionMode` | renders `chat.autopilot.enabled` (true when mode is `auto`) | — GAP: `approval_policy` is only passed through from `native.codex`, not derived from autonomy | — unknown: `--auto` is a CLI flag; no config key for it found in the schema | — n/a: pi asks for no approval (`security.md`) | renders `agentMode` (default `auto`), `toolPermission` (default `always-proceed`); checks `agentMode` non-empty |
| Sandbox (`autonomy.sandboxMode`, `native.codex.sandbox_mode`, `native.opencode.sandbox`, `native.antigravity.enableTerminalSandbox`) | — GAP: `sandbox` | — GAP: `sandbox.enabled` | — unknown: no sandbox setting verified | renders `sandbox_mode` from `native.codex` only; `autonomy.sandboxMode` is not read | renders `sandbox` from `native.opencode.sandbox`; key not in the published schema (see Findings) | — n/a: no sandbox in pi (`security.md`) | renders `enableTerminalSandbox` |
| Non-workspace access (`autonomy.allowNonWorkspaceAccess`, `native.antigravity.allowNonWorkspaceAccess`) | — unknown: no matching key verified | — unknown | — unknown | — unknown | — unknown | — n/a: no workspace boundary (`security.md`) | renders `allowNonWorkspaceAccess` (from `native.antigravity`) |
| Subagents (`autonomy.allowSubagentInvocations`, `subagentDepth`, `askTimeout`) | — unknown: no key verified | — GAP: `subagents.maxDepth` | renders `chat.subagents.allowInvocationsFromSubagents` | — GAP: `agents.max_depth` | — GAP: `experimental.subagent_depth` | — n/a: no built-in subagents in `pi --help` | — unknown |
| Allowed commands (`permissions.allowCommands`, `allowCommandPatterns`) | renders `permissions.allow`: fixed `WebSearch`, `WebFetch`, `Bash`, `Bash(gh pr merge * --auto*)` plus `Bash(<pattern>)`; `allowCommands` is not read | — GAP: `--allow-tool shell(...)` rules; no settings key found | renders `chat.tools.terminal.autoApprove` `true` entries from both lists | — GAP: `permissions` | — GAP: `permission` | — n/a: no permission system | renders `permissions.allow`: fixed `command(*)`, `read_url(*)`, `execute_url(*)`, `mcp(*)`, `command(gh pr merge * --auto*)` plus `command(<prefix>)` for patterns without an inner `*` |
| Denied commands (`denyCommandPatterns`, `denyCommandExact`, `denyCommandPrefixes`) | renders `permissions.deny`: `Bash(pat)`, `Bash(exact)`, `Bash(pfx:*)`; on Windows also `PowerShell(pfx *)` | — GAP: `--deny-tool shell(...)` rules; no settings key found | renders `chat.tools.terminal.autoApprove` `false` for exact and patterns; prefixes are not mapped (GAP) | — GAP: `permissions` | — GAP: `permission` | — n/a | renders `permissions.deny`: `command(pat)`, `command(exact)`, `command(pfx)` and `command(pfx *)`; `git push` patterns are deduplicated by prefix |
| Ask commands (`askCommandPrefixes`) | renders `permissions.ask`: `Bash(pfx)`, `Bash(pfx *)` | — GAP: no settings key found | renders `autoApprove` `false` for the prefix | — GAP: `permissions` | — GAP: `permission` | — n/a | renders `permissions.ask`: `command(pfx)`, `command(pfx *)` |
| Allowed URLs (`permissions.allowedUrls`) | — GAP: `WebFetch(domain:...)` rules; a fixed `WebFetch` allow is rendered instead | — GAP: `allowedUrls` (settings.json) | renders `chat.tools.urls.autoApprove` `true`; checks the key exists | — GAP: `permissions` network rules | — GAP: `permission` | — n/a | — GAP: `read_url(...)` rules; a fixed `read_url(*)`/`execute_url(*)` allow is rendered instead |
| Protected paths (`denyRead`, `denyWrite`) | renders `permissions.deny`: `Read(path)`, `Edit(path)` | — GAP: `sandbox.userPolicy.filesystem.deniedPaths` (sandbox only) | — unknown: no matching setting verified | — GAP: `permissions` filesystem rules | — GAP: `permission` | — n/a | renders `permissions.deny`: `read_file(path)`, `write_file(path)` |
| Allowed MCP servers (`permissions.allowedMcp`) | — GAP: `allowedMcpServers` | — unknown | — unknown | — unknown | — unknown | — n/a | renders fixed `mcp(*)` allow; the field is not read |
| Model tiers and effort (`models.tiers`, `defaultEffortLevel`, `showThinkingSummaries`) | — GAP: `model`, `effortLevel`, `showThinkingSummaries` are not rendered | renders `subagents.agents.<name>.{model,effortLevel}` from `native.copilot.agents` only; tiers not rendered (GAP: `model`, `effortLevel`) | — unknown | renders `model`, `model_reasoning_effort` from `native.codex` only; tiers not rendered | renders `model` from `native.opencode.defaultModel`; tiers not rendered (GAP: `small_model`) | renders `defaultProvider` (default `openai`), `defaultModel`, `defaultThinkingLevel` (default `medium`), `packages`; checks provider/model non-empty (always true because of defaults) | — unknown: `--model`/`--effort` are CLI flags; no settings key verified |
| Context compaction (`context.autoCompact`, `autoCompactWindow`, `promptCacheTtl`, `subagentPromptCacheTtl`, `maxTurnTokens`, `maxTokens`) | — GAP: `autoCompactEnabled` (schema-only) | — unknown | — unknown | — GAP: `model_auto_compact_token_limit` | — GAP: `compaction` | — GAP: `compaction.enabled`, `reserveTokens` | — unknown |
| Terminal bell (`ui.terminalBell`, `notifications.terminalBell`) | renders `preferredNotifChannel` (`terminal_bell` or `notifications_disabled`); checks it equals `terminal_bell` when the bell is on | renders `beep`; checks `beep=true` when the bell is on | — unknown | renders `tui.notifications`, `tui.notification_method=bel`; checks `tui.notifications=true` when the bell is on | renders `bell`; checks `bell=true` (key not in the published schema, see Findings) | — unknown | — unknown |
| Theme (`ui.theme`) | — GAP: `theme` (schema-only) | — GAP: `theme` | renders `workbench.colorTheme` (default `Catppuccin Mocha`) | — GAP: `tui.theme` | — unknown: theme is not in `opencode.json` | — GAP: `theme` | — unknown |
| Other UI (`tuiMode`, `viewMode`, `outputStyle`, `showTurnDuration`, `progressBar`, `timeFormat`) | — GAP: `tui`, `viewMode`, `outputStyle`, `showTurnDuration`, `terminalProgressBarEnabled` (schema-only) | — GAP: `terminalProgress` | — unknown | — unknown | — unknown | — GAP: `tuiMode`, `terminal.showTerminalProgress` | — unknown |
| Telemetry (`telemetry.*`, dual tier, hooks bridge) | — GAP: OTEL settings under `env` (schema-only) | — GAP: `telemetry.*` | — unknown | — GAP: `otel` | — GAP: `experimental.openTelemetry` | — n/a: only anonymous install telemetry (`enableInstallTelemetry`), not OTLP | — unknown |
| LSP (`lsp.enabled`, `lsp.server.*`) | checks each server names a row in `packages.toml`; not rendered (no `lspServers` key in the schema) | checks `packages.toml` row; not rendered (GAP: `lsp` configuration) | checks `packages.toml` row; native key unknown | checks `packages.toml` row; native key unknown | checks `packages.toml` row; not rendered (GAP: `lsp`) | checks `packages.toml` row; n/a: no built-in LSP | checks `packages.toml` row; native key unknown |
| Voice (`voice.enabled`, `mode`, `autoSubmit`, `language`) | renders `voice.enabled`, `voice.mode` (default `hold`), `voice.autoSubmit`; no `language` (it also sets the reply language); checks the Claude template reads `.agents.voice.{enabled,mode,autoSubmit}` and sets no `"language":` | — GAP: Copilot CLI has a documented `voice.enabled` key; not mapped yet | renders `dictation.enabled`, `agents.voice.language`; checks the static `dot_config/Code/User/settings.json` matches both | — n/a: no dictation in the CLI; `/voice` starts a realtime spoken conversation (cloud), not dictation into the prompt | — n/a: none found | — n/a: none built in (community packages only) | — n/a: `/voice` dictation (cloud) has no documented settings key |
| Native escapes (`native.*`) | renders `defaultMode`, `skipAutoPermissionPrompt`; `cleanupPeriodDays`, `disableClaudeAiConnectors` are parsed, not rendered (GAP: `cleanupPeriodDays`, `disableClaudeAiConnectors`) | renders `subagents.agents` from `native.copilot.agents`; checks binding has model and `effortLevel` | — n/a: no native block | renders `model`, `model_reasoning_effort`, `approval_policy`, `sandbox_mode` when set | renders `model`, `sandbox` | renders the four `native.pi` keys | renders `agentMode`, `toolPermission`, `enableTerminalSandbox`, `allowNonWorkspaceAccess` |
| Profiles (`profiles.*`) | `WithProfile` is harness-independent: merges tiers, appends allow/deny/MCP lists, overrides `executionMode`, `sandboxMode`, `ui`. It drops `allowCommandPatterns`, `askCommandPrefixes`, other autonomy fields and `profiles.*.plugins`. | same | same | same | same | same | same |

Sources (Configuration): `internal/config/policy.go` (fields, `WithProfile`), `internal/config/validate.go` (`Validate`: autonomy mode enum, non-empty patterns, tier model/effort, `native.claude.defaultMode`, `native.antigravity.agentMode`, `native.pi.defaultThinkingLevel`, voice mode and BCP 47 language, LSP server shape), `internal/translate/{claude,copilot,vscode,codex,opencode,pi,antigravity}.go`, `internal/parity/parity.go: CheckParity`, `internal/parity/files.go: CheckSourceFiles`.

## Instructions and MCP

| Dimension | Claude Code | Copilot CLI | VS Code Copilot | Codex | opencode | pi | Antigravity |
|---|---|---|---|---|---|---|---|
| System prompt / instructions (`agents prompt`, `agents render -part prompt`) | reads `CLAUDE.md`, `.claude/CLAUDE.md`, then `~/.claude/CLAUDE.md` | reads `.github/copilot-instructions.md`, `.copilot-instructions.md`, then `~/.copilot/copilot-instructions.md`, `~/.config/github-copilot/copilot-instructions.md` | — GAP: VS Code Copilot has custom instructions files; `agents prompt` rejects `vscode` | reads `AGENTS.md`, `.codex/AGENTS.md`, then `~/.codex/AGENTS.md` | reads `AGENTS.md`, `.config/opencode/AGENTS.md`, then `~/.config/opencode/AGENTS.md` | reads `AGENTS.md`, `.pi/AGENTS.md`, then `~/.pi/agent/AGENTS.md`, `~/.pi/AGENTS.md` | reads `AGENTS.md`, `.gemini/AGENTS.md`, then `~/.gemini/antigravity-cli/AGENTS.md`, `~/.gemini/config/AGENTS.md` |
| Prompt composition and presence check | All harnesses: when no file is found, falls back to `profiles.<name>.instructions` or the template prompt layers, prints it (`render -part prompt`); `agents prompt -check` checks presence only. `agents check` does not check prompts. | same | same | same | same | same | same |
| MCP (`agents mcp sync`, `agents mcp add`, `.mcp.json` source) | — n/a: `.mcp.json` is Claude Code's native project file, so there is nothing to write | — unknown: native MCP file not verified | — unknown: native MCP file not verified | syncs `<workspace>/.codex/config.toml` | — GAP: `mcp` in `opencode.json` | syncs `<workspace>/.pi/mcp.json` | syncs `<workspace>/.agents/mcp_config.json` |
| MCP safety (`agents mcp sync -check`) | n/a | n/a | n/a | checks source validity (name, URL, placeholders) and target preflight: no symlinks in the path, existing file carries the `agents mcp sync` marker | n/a | same as Codex | same as Codex |

Sources (Instructions and MCP): `internal/cli/harness_prompt.go` (`isKnownHarness`, `workspacePromptCandidates`, `homePromptCandidates`), `internal/cli/prompt.go`, `internal/mcpsync/mcpsync.go` (`Sync`, `Check`, `PreflightCheck`, `isOwned`), `internal/mcpsync/render.go`, `internal/cli/mcp.go`. Written targets are mode `0600`, JSON targets carry `_generatedBy`, TOML targets a `# Generated by` header. `Policy.MCP` is parsed but no code reads it.

## Plugins

| Dimension | Claude Code | Copilot CLI | VS Code Copilot | Codex | opencode | pi | Antigravity |
|---|---|---|---|---|---|---|---|
| Registry config (`harnessPlugins`, `harnessStandalone`, `allowedHarnesses`) | checks plugin names against the registry and harness names against the allowed list | same | checks as a valid harness name; install state is read through the Copilot column | same | same | same | same |
| Installed state (`plugins list`) | reads `~/.claude/plugins` installed version | reads `~/.copilot/installed-plugins`, `~/.copilot/config.json`, `~/.copilot/settings.json` | reads `~/.vscode/agent-plugins`, folded into the Copilot column | reads `~/.codex/plugins/cache/<ns>/<plugin>` | reads copies in `$XDG_CONFIG_HOME/opencode` and compares them with the checkout | reads `packages` in `~/.pi/agent/settings.json`; installed only if an entry resolves to the checkout | reads the Antigravity plugin directory |
| Update / install (`plugins update`) | `claude plugin marketplace update`, then `plugin update`/`plugin install <name>@<ns>` | same with the `copilot` binary | — GAP: no update command; only the Copilot CLI path is driven | `codex plugin marketplace upgrade`, `plugin add <name>@<ns>`, then the checkout's `scripts/install.mjs codex` | the checkout's `scripts/install.mjs opencode` | `pi install <checkout>` only when a plugin is missing; stale plugins follow the checkout pull | `agy plugin install <plugin dir>` plus `scripts/install.mjs antigravity` |
| Prune (`plugins prune`) | removes superseded versions in `~/.claude/plugins/cache/<ns>` | — n/a: no prune target in code | — n/a | removes superseded versions in `~/.codex/plugins/cache/<ns>` | — n/a | — n/a | removes the obsolete `api` plugin in `~/.gemini/config/plugins` |

Sources (Plugins): `internal/pluginreg/pluginreg.go` (`CheckRegistry`, `DefaultHarnesses`), `internal/pluginmgmt/pluginmgmt.go` (`List`, per-harness `*Version`, `FindPruneCandidates`), `internal/cli/plugins.go`, `internal/cli/plugins_update.go: harnessSteps`. `agents check` validates the plugin registry config only; installed state is `plugins list`.

## Runtime tools

| Dimension | Claude Code | Copilot CLI | VS Code Copilot | Codex | opencode | pi | Antigravity |
|---|---|---|---|---|---|---|---|
| Status line (`agents statusline [claude\|copilot\|tmux\|omp]`) | renders text for a `statusLine` command (same output for every target); the settings key is not rendered | renders the same text; settings key not rendered (GAP: `statusLine`) | — unknown | — GAP: `tui.status_line` | — unknown | — unknown | — unknown |
| Usage and quota (`agents usage`) | reads Anthropic credentials, queries `api.anthropic.com` | reads GitHub Copilot credentials, queries `api.github.com` | — unknown: `provider.Harnesses` has no VS Code entry; it is not known whether VS Code's GitHub login is discovered | reads OpenAI credentials, queries `chatgpt.com`; falls back to Codex rollout sessions | reads per-provider logins in opencode's auth store, queries the provider (Anthropic, OpenAI, GitHub Copilot) | reads per-provider logins in pi's `auth.json` (commands starting with `!` are never run) | reads no credential; runs `agy` for `/usage`, falls back to the agy log |
| Doctor (`agents doctor`) | reads tier models from the policy and the usage cache; reports unknown model, tier drift, missing account, capacity, headroom, better model | reads `native.copilot.agents` models (fallback to tiers) and the same checks | — n/a: `agents` manages no VS Code model setting | reads tiers and `native.codex.model` | reads tiers and `native.opencode.defaultModel` | reads tiers and `native.pi.defaultModel` | reads tiers |
| Models catalog (`agents models`) | Harness-independent: lists, syncs and renders the model catalog (models.dev, LMArena, Epoch AI); `doctor` joins it to the settings above | same | same | same | same | same | same |
| Init (`agents init`) | scaffolds `agents.toml` with the detected harnesses from `claude, copilot, antigravity, codex, opencode, pi` | same | — GAP: not in the init harness list | same | same | same | same |

Sources (Runtime tools): `internal/cli/statusline.go`, `internal/cli/usage*.go` and `internal/provider/*.go` (credential discovery), `internal/usage/*.go`, `internal/doctor/{doctor,checks,resolve}.go` (`harnessOrder` has no VS Code), `internal/catalog/*.go`, `internal/cli/init.go`.

## Findings

- `opencode.ai/config.json` has no top-level `bell` or `sandbox` key, but `OpenCodeSettings` emits both and `CheckParity` tests `bell`. Whether opencode ignores them or rejects the file is not verified.
- `CheckParity` renders the policy and tests its own output, so for Pi (`defaultModel`/`defaultProvider` default to non-empty values) and Antigravity (`agentMode` defaults to `auto`) the checks cannot fail.
- The Claude settings schema checked (schemastore snapshot) lists no `skipAutoPermissionPrompt`, `autoCompactWindow` or `lspServers`; `ClaudeSettings` still emits the first.
- Codex's `realtime` and `audio` tables and the `tui.keymap.chat.toggle_voice` binding belong to `/voice`, a realtime spoken conversation (checked in codex-cli 0.167). It is not dictation into the prompt, so the policy's dictation fields have no Codex target.

## Coverage summary

Every GAP above, once, ranked by user impact. Format: harness - dimension - native key or feature - what `agents` would need.

Security and permissions

1. Copilot CLI - allowed and denied commands, URLs, protected paths - `allowedUrls`, `deniedUrls` (settings.json), `--allow-tool`/`--deny-tool` rules, `sandbox.userPolicy.filesystem.deniedPaths` - a `CopilotSettings` mapping for `allowedUrls`/`deniedUrls` and a decision where tool rules live (flags or `permissions-config.json`).
2. opencode - all permission rules - `permission` - an `OpenCodeSettings` mapping from allow/deny/ask lists.
3. Codex - command, URL and path rules - `permissions`, `default_permissions` - a `CodexConfig` mapping from the permission lists.
4. Codex - autonomy to approval and sandbox - `approval_policy`, `sandbox_mode` - derive them from `autonomy.executionMode`/`sandboxMode` instead of only `native.codex`.
5. VS Code Copilot - denied command prefixes - `chat.tools.terminal.autoApprove` - map `denyCommandPrefixes` to `false` entries like exact and pattern denies.
6. Claude Code - allowed URLs and MCP servers - `WebFetch(domain:...)` rules, `allowedMcpServers` - map `allowedUrls` and `allowedMcp` instead of the fixed blanket allow.
7. Antigravity - allowed URLs and MCP servers - `read_url(...)`, `mcp(...)` rules - map `allowedUrls` and `allowedMcp` instead of the fixed `*` allow.
8. Claude Code, Copilot CLI - sandbox - `sandbox` (Claude), `sandbox.enabled` (Copilot) - map `autonomy.sandboxMode`.
9. Copilot CLI - execution mode - `defaultMode`, `defaultPermissionMode` - map `autonomy.executionMode`.

Correctness of what users see

10. Claude Code, Copilot CLI, opencode - model and effort - `model`, `effortLevel`, `showThinkingSummaries`; `model`, `effortLevel`; `small_model` - render `models.tiers` and the effort fields (today only `doctor` and `statusline` read tiers).
11. Copilot CLI - voice - `voice.enabled` - map `voice.enabled`; add a Copilot template check (in progress outside this branch).
12. Claude Code, Codex, opencode, pi - context compaction - `autoCompactEnabled`, `model_auto_compact_token_limit`, `compaction`, `compaction.*` - map `context.*`.
13. Claude Code, Copilot CLI, Codex, pi - theme - `theme`, `theme`, `tui.theme`, `theme` - map `ui.theme` (VS Code already is).
14. Claude Code, Copilot CLI, pi - other UI fields - `tui`, `viewMode`, `outputStyle`, `showTurnDuration`, `terminalProgressBarEnabled`; `terminalProgress`; `tuiMode`, `terminal.showTerminalProgress` - map `ui.*`.
15. Claude Code, Copilot CLI, Codex, opencode - telemetry - OTEL `env`, `telemetry.*`, `otel`, `experimental.openTelemetry` - map `telemetry.*`.
16. Copilot CLI, Codex, opencode - subagent limits - `subagents.maxDepth`, `agents.max_depth`, `experimental.subagent_depth` - map `autonomy.subagentDepth`.
17. Copilot CLI, opencode - LSP - `lsp` configuration - render `lsp.server.*` (today only the `packages.toml` row is checked).
18. Claude Code - native escapes - `cleanupPeriodDays`, `disableClaudeAiConnectors` - render the parsed `native.claude` fields.
19. VS Code Copilot - instructions - custom instructions files - accept `vscode` in `agents prompt`.
20. VS Code Copilot - CLI render - `agents render -harness vscode` - add the `vscode` case to `renderHarness`.

Convenience

21. Claude Code, Copilot CLI, Codex - status line setting - `statusLine`, `statusLine`, `tui.status_line` - render the key that calls `agents statusline`.
22. opencode - MCP - `mcp` in `opencode.json` - add an opencode target to `mcp sync`.
23. VS Code Copilot - plugin update - no command driven - add an update/install path.
24. VS Code Copilot - init - harness list - add `vscode` to `agents init`.

Derived from the code at 0066e38; checked 2026-11-11.
