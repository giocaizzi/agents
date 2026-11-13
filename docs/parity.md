# Harness parity matrix

What `agents apply` writes for each config key on each harness. Derived from `internal/harness`, not from other docs. Keys are described in [config.md](config.md).

## Reading the matrix

| Cell | Meaning |
|---|---|
| a native key or file | `agents apply` writes it (merged into the live file; only managed keys are touched) |
| check-only | compared by `apply` and `check -live`, never written |
| **—** | not written. `n/a:` means the harness has no such feature; `GAP:` means the harness supports it and `agents` does not cover it; `unverified:` means the evidence is missing |

Every harness also gets `harness.<name>.settings`, merged as-is into its native settings file, and the composed instruction layers.

## What each harness receives

| Harness | Files |
|---|---|
| Claude Code | `~/.claude/settings.json`, `~/.claude.json` (MCP servers), `~/.claude/CLAUDE.md`; with language servers, a local plugin under `<data>/claude-plugins` (marketplace `agents`, plugin `lsp@agents`) |
| Copilot CLI | `~/.copilot/settings.json`, `lsp-config.json`, `mcp-config.json`, `copilot-instructions.md` |
| VS Code | `settings.json` in the user directory (check-only); VS Code's agent chat reads `~/.copilot`, so it gets the Copilot files too. When `copilot` is also selected, `copilot` owns them |
| Codex | `~/.codex/config.toml`, `~/.codex/rules/agents.rules`, `~/.codex/AGENTS.md` |
| opencode | `~/.config/opencode/opencode.jsonc`, `~/.config/opencode/AGENTS.md` |
| pi | `~/.pi/agent/settings.json`, `mcp.json`, `AGENTS.md` |
| Antigravity | `~/.gemini/antigravity-cli/settings.json`, `~/.gemini/config/mcp_config.json`, `~/.gemini/config/AGENTS.md` |

JSON targets keep the live key order and append new keys; an unchanged target is not rewritten; a symlinked target is refused. The old local Claude marketplace keys (`lsp@dotfiles`) are removed once.

## Dimensions

| Config | Claude Code | Copilot CLI | VS Code | Codex | opencode | pi | Antigravity |
|---|---|---|---|---|---|---|---|
| `permissions` | `permissions` allow, deny and ask rules in Claude syntax; `defaultMode` from `harness.claude.default_mode` (default `auto`); `PowerShell(...)` deny rules on Windows | `allowedUrls` (union with the live list) | `chat.tools.terminal.autoApprove`, `chat.tools.urls.autoApprove` (check-only) | `rules/agents.rules`: `prefix_rule` for allow and deny patterns that are literal prefixes; others are skipped with a warning | `permissions` (allow and ask rules, including `shell` from `allow_commands`, `webfetch` from `allowed_urls` and `external_directory` from `allow_non_workspace_access`) and `policies` (deny) arrays, merged with the live ones | — (GAP: pi has no permission file) | `permissions` allow, ask and deny in Antigravity syntax; patterns with an inner `*` are skipped |
| `autonomy` | `askUserQuestionTimeout` from `ask_timeout` | — | — | `harness.codex.approval_policy`, `sandbox_mode` | `experimental.subagent_depth`, `permissions` `external_directory` | — | `agentMode` (from `execution_mode` unless set), `allowNonWorkspaceAccess` |
| `models.default` (session) | `model`, `effortLevel` | `model` | n/a: no session setting | `model`, `model_reasoning_effort` | `model` | `defaultModel`, `defaultThinkingLevel` | n/a: no session setting |
| `ui` | `theme`, `tui`, `viewMode`, `outputStyle`, `showTurnDuration`, `terminalProgressBarEnabled`, `timeFormat` | — | `workbench.colorTheme` (check-only) | — | — | — | — |
| `context` | `autoCompactWindow`, `promptCacheTtl`, `subagentPromptCacheTtl` | — | — | — | `compaction.auto` | — | — |
| `notifications` | `preferredNotifChannel` | `beep` | — | `tui.notifications` with `bel` | — | — | — |
| `voice` | `voice` object ([voice.md](voice.md)) | `voice.enabled` | `dictation.enabled`, `agents.voice.language` (check-only) | n/a | n/a | n/a | — (no documented key) |
| `telemetry` | `env` OpenTelemetry keys; removed when off | — | — | `otel` exporters (OTLP over HTTP) | — | — | — |
| `lsp` | local `lsp@agents` plugin | `lsp-config.json` | — | — | `lsp` | — | — |
| `mcp.servers` | `~/.claude.json` `mcpServers` | `mcp-config.json` | via the Copilot files | `mcp_servers` | `mcp.servers` | `mcp.json` | `mcp_config.json` |
| `plugins` | `enabledPlugins`, `extraKnownMarketplaces` | `enabledPlugins`, `extraKnownMarketplaces` | via the Copilot files | — (plugins are installed by `agents plugins`) | — (installer from the registry checkout) | `packages` (registry checkout plus `harness.pi.packages`) | — |

A harness's own keys (`harness.<name>.*`) are written where the harness has a matching setting; see the typed keys in [config.md](config.md#harnessname).

### MCP OAuth clients

| Harness | Written for `oauth.*` |
|---|---|
| Claude Code | `oauth.clientId`, `oauth.callbackPort` (the secret stays in the keychain) |
| Copilot CLI | `oauthClientId`, `oauthPublicClient`, `auth.redirectPort`. unverified: `auth.redirectPort` is not among the documented remote-server fields of the Copilot CLI reference |
| Codex | `oauth.client_id`, `oauth.callback_port` |
| pi | `oauth.clientId`, `oauth.callbackPort` |
| Antigravity | `oauth.clientId`; no port setting, a configured port is left out with a warning |
| opencode | `oauth.client_id`, `oauth.callback_port` |

The redirect host each harness sends is in [cli.md](cli.md#oauth-redirect-hosts).

### Workspaces

`agents mcp sync` (and `agents apply`, for configured workspaces) writes MCP files under a workspace root: `.mcp.json` for Claude Code, Copilot CLI and VS Code (shared, with both OAuth styles), `.codex/config.toml`, `.pi/mcp.json`, `opencode.json` (servers with an OAuth client are left out: no verified translation) and `.agents/mcp_config.json`. In Codex's user config each workspace path is also marked `trust_level = "trusted"`.

## Models

`agents` never edits plugin files. A per-agent model is written only through a harness's own user-level setting; where a harness has none, the plugin's model applies.

| Harness | Per agent (`models.assign`) | Session (`models.default`) |
|---|---|---|
| Claude Code | n/a: no settings-level per-subagent override; the plugin's frontmatter model applies | `model`, `effortLevel` |
| Copilot CLI | `subagents.agents."<plugin>:<agent>"` = `{model, effortLevel}` in `~/.copilot/settings.json` | `model` |
| opencode | `agents.<agent>.model` in `opencode.jsonc` (the bare agent name; no effort field) | `model` |
| Codex | n/a: would mean rewriting plugin-generated files | `model`, `model_reasoning_effort` |
| pi | n/a: same | `defaultModel`, `defaultThinkingLevel` |
| VS Code, Antigravity | n/a | n/a |

A tier names one native model id per harness. A harness without an id for a tier keeps its plugin or session default; there is no translation between model namespaces. Typed harness keys (`harness.codex.model`, `harness.pi.default_model`) win over the default tier. `agents models tiers` prints the effective result; `agents check` reports an unknown tier as an error, an assignment matching no installed agent as a warning, and an assignment a harness cannot honour as info.
