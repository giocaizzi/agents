# Configuration reference

`agents` reads one TOML file and writes the native files of every selected harness from it. This page lists every key, how the layers combine, and where files live. The committed JSON Schema is [`schema/config.schema.json`](../schema/config.schema.json); `agents schema` prints the one generated from the running binary. Every key is optional. Unset means "agents manages nothing for it". Which harnesses honour each key is in the [feature matrix](features.md).

## Files and directories

| What | Linux and macOS | Windows |
|---|---|---|
| Config (`config.toml`, `instructions/`) | `$XDG_CONFIG_HOME/agents`, else `~/.config/agents` | `%APPDATA%\agents` |
| Cache (`models.json`, usage cache) | `$XDG_CACHE_HOME/agents`, else `~/.cache/agents` | `%LOCALAPPDATA%\agents\cache` |
| State (`apply.json`, registry pull stamps) | `$XDG_STATE_HOME/agents`, else `~/.local/state/agents` | `%LOCALAPPDATA%\agents\state` |
| Data (registry checkouts, local Claude plugin) | `$XDG_DATA_HOME/agents`, else `~/.local/share/agents` | `%LOCALAPPDATA%\agents\data` |

macOS uses the XDG layout, not `~/Library`. A set `XDG_*` variable wins on every OS. `AGENTS_CONFIG` names another config file; it must exist.

The first lines of a config file are the schema comment and the version:

```toml
#:schema https://raw.githubusercontent.com/giocaizzi/agents/main/schema/config.schema.json
schema_version = 1
```

`schema_version` must be `1`. Decoding is strict and keys are `snake_case`: an unknown key is an error that names the file and line (`config.toml:12: unknown key "colour"`).

## Precedence

Lowest to highest:

1. defaults (`schema_version = 1`, `instructions.dir` = `<config dir>/instructions`)
2. the user `config.toml` (or the file named by `AGENTS_CONFIG` or `-file`)
3. the `[os.<goos>]` overlay for the running OS (`darwin`, `linux`, `windows`)
4. the selected profile's overlay (`[profiles.<name>]`)
5. environment: `AGENTS_PROFILE` and `AGENTS_HARNESSES` (`AGENTS_CONFIG` chooses the file of layer 2)
6. flags: `-file`, `-profile`, `-harnesses`

There is no project-level config file: a directory's own `agents.toml` is never read, so cloning a repository cannot change what `agents apply` writes into your home directory ([design.md](design.md#project-config-is-not-read)).

Tables merge key by key; scalars and arrays replace. `agents config show -origin` prints the layer that set each key.

The profile is chosen by `-profile`, else `AGENTS_PROFILE`, else the `profile` key; naming an undeclared profile is an error. A profile's `instructions` list and each `harness.<h>.instructions` list inside it are composition data and never merge into the config (see Instructions).

Harnesses: when `harnesses` is unset in every layer, agents selects those whose home exists (`~/.claude`, `~/.copilot`, the VS Code user directory, `~/.codex`, `~/.config/opencode`, `~/.pi`, `~/.gemini/antigravity-cli`). `config show -origin` reports `auto-detect`. Valid names: `claude`, `copilot`, `vscode`, `codex`, `opencode`, `pi`, `antigravity`.

## Variables, paths and secrets

`${VAR}` (only that form) is expanded at `apply` time in: user-level MCP server `url`, `command`, `args`, `env`, `headers` and `oauth.client_id`; workspace paths; `instructions.dir`; every string inside `harness.<h>.settings`; `telemetry.endpoint`. An unset or empty variable skips that entry and prints a warning (`mcp.servers.docs: skipped, ${DOCS_TOKEN} is unset`); the rest of the apply proceeds.

`$${VAR}` is the escape: it yields the literal text `${VAR}`, for a value the harness expands itself later (for example a hook command that reads `${CLAUDE_PROJECT_DIR}`).

Paths expand a leading `~`, `~/` or `~\` (the Windows form works on every OS) to the home directory.

Where secrets can land:

- **User-level files** (`~/.claude.json`, `~/.codex/config.toml`, ...) receive expanded values. A new file is created with mode `0600`; the mode of an existing file is kept. Diffs print every expanded secret as `redacted`.
- **Workspace files** (`agents mcp sync` and `mcp.workspaces`: `.mcp.json`, `.codex/config.toml`, `.pi/mcp.json`, `opencode.json`, `.agents/mcp_config.json`) usually get committed, so they never hold an expanded value. A `${VAR}` stays a reference where the harness expands it itself (Claude Code, Copilot CLI and VS Code in `.mcp.json`; opencode as `{env:VAR}`); for Codex, pi and Antigravity, which do not expand references in a project file, the server is skipped with a warning. A workspace with `expand_env = true` is the opt-in exception (see [MCP](#mcp)).
- OAuth client secrets have no config key: they live only in the harness's own keyring.

Which keys each harness writes, and which it has no setting for, is in the [feature matrix](features.md); `agents check` reports a key set in the config that a selected harness ignores.

## Top level

| Key | Type | Meaning |
|---|---|---|
| `schema_version` | int | Must be `1` |
| `profile` | string | Selected profile; a machine fact. Must be declared under `profiles` |
| `harnesses` | array of harness names | Harnesses to configure; unset means auto-detect |

## `[autonomy]`

| Key | Type | Meaning |
|---|---|---|
| `execution_mode` | `ask`, `auto` or `full` | How freely agents act. `ask`: asks before edits and commands. `auto`: proceeds, and the harness's own safety layer still asks where it decides to. `full`: never asks |
| `sandbox_mode` | `off`, `workspace` or `read-only` | Sandbox for agent commands: none, writes confined to the workspace, or no writes |
| `allow_non_workspace_access` | bool | Allow access outside the workspace (Antigravity, opencode) |
| `subagent_depth` | int | Maximum subagent nesting (opencode) |
| `allow_subagent_invocations` | bool | Allow agents to invoke subagents |
| `ask_timeout` | string | How long to wait for an answer (Claude `askUserQuestionTimeout`) |


Any other value of the two modes is rejected. Each harness maps them to its own native settings, listed with their sources in [features.md](features.md) under each harness's rendered keys. A value with no exact native equivalent on a harness is not approximated: nothing is written for that harness and `agents apply` warns (for example `execution_mode = "auto"` on VS Code). The one exception is `sandbox_mode = "read-only"` on a harness with only an on/off sandbox (Claude Code, Copilot CLI, VS Code, Antigravity): it turns the sandbox on, approximate only toward the stricter side, and `agents apply` prints a `note:` saying so. On Claude Code, a managed sandbox also writes `sandbox.autoAllowBashIfSandboxed = false` unless the effective `defaultMode` (`harness.claude.default_mode`, else the `execution_mode` mapping) is `auto` or `bypassPermissions`, so the sandbox does not skip the prompts a mode that asks (or an unset one) wants. `harness.antigravity.enable_terminal_sandbox` is tri-state: an explicit `false` wins over `sandbox_mode = "workspace"`, and `harness.antigravity.tool_permission` is set from `execution_mode = "full"` only when `harness.antigravity.agent_mode` is unset. The harness's own typed field (`harness.claude.default_mode`, `harness.codex.approval_policy`, `harness.codex.sandbox_mode`, `harness.antigravity.agent_mode`, `harness.antigravity.tool_permission`, `harness.antigravity.enable_terminal_sandbox`) wins over the shared value. opencode and pi have no such settings, so the keys do nothing there.

## `[permissions]`

Shared rules, translated to each harness's own syntax (Claude `Bash(...)`/`Read(...)`/`Edit(...)`, Antigravity `command(...)`, Codex rules file, opencode permissions and policies, VS Code auto-approve maps). A rule a harness cannot express is left out with a warning. When that rule is a deny, Codex and Antigravity (which match command prefixes only) also leave out every allow prefix that could cover a command the deny forbids, such as `git` against `git push * HEAD:main`, and warn for each, so a skipped deny never leaves its command allowed.

| Key | Type | Meaning |
|---|---|---|
| `allow_commands` | array of string | Commands allowed without asking |
| `allowed_urls` | array of string | URLs agents may fetch (merged as a union with the live list in Copilot) |
| `allow_command_patterns` | array of string | Regular expressions of allowed commands |
| `deny_read`, `deny_write` | array of string | Path globs agents may not read or write |
| `deny_command_patterns` | array of string | Regular expressions of denied commands |
| `deny_command_exact` | array of string | Exact command lines that are denied |
| `deny_command_prefixes` | array of string | Denied command prefixes |
| `ask_command_prefixes` | array of string | Prefixes that always ask |
| `allowed_mcp` | array of string | MCP servers or tools allowed without asking |
| `reasons` | table of string | Message shown when a rule matches, keyed by a pattern listed in one of the command lists above (validated; the [feature matrix](features.md) lists the harnesses that render it) |

## `[models]`

| Key | Type | Meaning |
|---|---|---|
| `default_effort_level` | string | Default reasoning effort (Claude `effortLevel`) |
| `show_thinking_summaries` | bool | Show thinking summaries |
| `default` | string | Tier of each harness's main session model; must be a declared tier |
| `tiers.<name>` | table | A free-named tier |
| `assign.<key>` | string | Tier for an agent id (`<plugin>:<agent>`); globs allowed, an exact key wins over a glob; the tier must be declared |
| `catalog` | table | Model catalog preferences |

Tier (`models.tiers.<name>`), all keys optional:

| Key | Type | Meaning |
|---|---|---|
| `claude`, `copilot`, `vscode`, `codex`, `opencode`, `pi`, `antigravity` | string | Native model id for that harness (opencode uses `provider/id`). A harness with no id keeps its plugin or session default |
| `effort` | string | Reasoning effort for every harness |
| `efforts.<harness>` | string | Effort for one harness; overrides `effort`. Keys must be harness names |

Catalog (`models.catalog`), used by `agents models` and `agents doctor`:

| Key | Type | Meaning |
|---|---|---|
| `providers` | array of string | Providers to include |
| `since` | string | Ignore models released before this date |
| `pins.<name>` | string | Pinned model per tier or role |
| `ignore` | array of string | Models to ignore |
| `aliases.<provider>.<name>` | string | Model aliases per provider |

### Model tiers

A tier names one native model id per harness. `models.default` names the tier of each harness's main session model; `models.assign` maps agent ids (`<plugin>:<agent>`, globs allowed) to tiers. A harness without an id for a tier keeps its plugin or session default: model ids are never translated between harnesses. Typed harness keys (`harness.codex.model`, `harness.pi.default_model`) win over the default tier. `agents models tiers` prints the effective result; `agents check` reports an unknown tier as an error. How tiers reach each harness is in [parity.md](parity.md#models); the ownership split is in [design.md](design.md#model-ownership-split).

## `[context]`, `[ui]`, `[notifications]`, `[voice]`

| Key | Type | Meaning |
|---|---|---|
| `context.auto_compact` | bool | Compact the context automatically |
| `context.auto_compact_window` | int | Context size that triggers compaction |
| `context.prompt_cache_ttl` | string | Prompt cache lifetime |
| `context.subagent_prompt_cache_ttl` | string | Prompt cache lifetime for subagents |
| `context.max_turn_tokens`, `context.max_tokens` | int | Token limits of a turn and of a response |
| `ui.theme` | string | Colour theme (VS Code theme falls back to a default when unset) |
| `ui.tui_mode`, `ui.view_mode`, `ui.output_style`, `ui.time_format` | string | Terminal UI preferences |
| `ui.show_turn_duration`, `ui.progress_bar` | bool | Show turn duration; show a progress bar |
| `notifications.terminal_bell` | bool | Ring the terminal bell when a turn ends |
| `voice.enabled` | bool | Enable voice input |
| `voice.mode` | string | `hold` or `tap` |
| `voice.auto_submit` | bool | Submit the transcript automatically |
| `voice.language` | string | Dictation language (BCP 47) |

Which harnesses honour which voice fields is in [voice.md](voice.md) and the [feature matrix](features.md).

## `[telemetry]`

| Key | Type | Meaning |
|---|---|---|
| `enabled` | bool | Export telemetry |
| `harnesses` | list | Harnesses that export telemetry (unset: every selected harness) |
| `endpoint` | string | Collector endpoint (`${VAR}` expands) |
| `protocol`, `temporality` | string | Export protocol and metric temporality |
| `log_tool_details`, `log_tool_content` | bool | Log tool call details and content |
| `service_namespace` | string | Service namespace resource attribute |
| `attributes.<name>` | string | Extra resource attributes |
| `services.<harness>` | string | Service name per harness (default: the harness name) |

With telemetry off, agents removes the Claude telemetry `env` keys it set earlier.

## `[lsp]`

| Key | Type | Meaning |
|---|---|---|
| `enabled` | bool | Make language servers available |
| `servers.<name>.command` | array of string | Command line (argv); required |
| `servers.<name>.extensions.<ext>` | string | File extension (`.go`) to language id |
| `servers.<name>.filetypes` | array of string | Editor file types; default: the distinct language ids |
| `servers.<name>.format` | bool | Use the server to format |

A server is configured only when `command[0]` is found on `PATH`; on Windows a `.cmd` or `.bat` shim runs through `cmd /c`. Skipped servers are reported as warnings.

## `[instructions]`

| Key | Type | Meaning |
|---|---|---|
| `dir` | string | Directory of layer files; default `<config dir>/instructions` |
| `edit_command` | array of string | Argv for `agents prompt edit`; `{name}` and `{file}` are replaced. Without it `$EDITOR` is used |
| `vars.<name>` | any | Template variables, available as `.vars.<name>` |
| `layers.<name>.file` | string | File of the layer, relative to `dir`; default `<name>.md` |

Layer lists are `profiles.<p>.instructions` (every harness of that profile), `harness.<h>.instructions` (every profile of that harness) and `profiles.<p>.harness.<h>.instructions` (one cell). Every name must be declared under `instructions.layers`. See [cli.md](cli.md#editing-instructions).

## `[plugins]`

| Key | Type | Meaning |
|---|---|---|
| `registry` | string | Plugin registry repository (`owner/repo`); plugin sources are configuration, never built in |
| `enabled.<plugin>` | array of harness names | Harnesses the plugin is enabled for |
| `disabled` | array of string | Plugins that stay off (written as disabled) |
| `standalone.<name>`, `external.<name>` | table | Plugins outside any registry: `source`, `path`, `harnesses` (array), `version` |

The registry is checked out under `<data>/registries/<owner>/<repo>`; `agents plugins update` pulls it at most every 24 hours. pi receives that checkout as a package, and the opencode installer runs from it. `harness.pi.packages` lists only extra pi packages.

## `[mcp]`

| Key | Type | Meaning |
|---|---|---|
| `servers.<name>` | table | User-level servers, written to every selected harness |
| `workspaces.<name>.path` | string | Workspace directory (required) |
| `workspaces.<name>.servers.<server>` | table | Servers synced to that workspace's native files |
| `workspaces.<name>.expand_env` | bool | Write expanded `${VAR}` values instead of references, for every harness (default `false`) |

`expand_env = true` writes the expanded values of that workspace's servers into its files for every harness, so Codex, pi and Antigravity get the servers back and the skip warning no longer fires. The values, secrets included, land in plaintext in files written `0600`; `apply -n` and `render` still redact them. Use it for a workspace outside any repository, such as a scratch ops folder. `check`, `apply` and `mcp sync` refuse it (error at `mcp.workspaces.<name>.expand_env`) when the expanded workspace path is inside a git work tree (a `.git` entry in the path or any ancestor), since the files could be committed. Without it, workspace files keep references as described above.

Limits of that check:

- Symlinks are resolved on the nearest existing ancestor of the path, and both the path as written and the resolved one are checked.
- Only a `.git` entry in the path or an ancestor is detected. A bare repository whose work tree is elsewhere (`core.worktree`, or `GIT_DIR` / `--work-tree` setups that track a home directory) is not.
- The check runs before the write, so there is a small window between the two.
- Command deny patterns with a `*` (such as `git push * HEAD:main`) match by position on the command line, on every harness: an argument shift such as `git -C x push origin HEAD:main` is not matched by `git push * HEAD:main`, and matching is case-sensitive and word-based, so case and quoting differences are not caught either.

A server (user or workspace) has exactly one of `url` or `command`:

| Key | Type | Meaning |
|---|---|---|
| `type` | string | Transport; inferred from `url` or `command` |
| `url` | string | Remote server URL |
| `headers.<name>` | string | HTTP headers |
| `command`, `args`, `env.<name>` | string, array, string | Local server command line and environment |
| `oauth.client_id` | string | Pre-registered OAuth client id (`url` servers only; no control characters, quotes or backslashes) |
| `oauth.callback_port` | int | Local callback port, 1 to 65535 |
| `oauth.public_client` | bool | The client has no secret |

Profile overlays may carry partial servers (a field added to a server declared elsewhere); the merged config and workspaces must be complete.

## `[harness.<name>]`

One table per harness. `instructions` (array of layer names) exists on all; `settings.<key>` is merged as-is into the harness's native settings file (nested tables merge, other values replace, `${VAR}` expands).

| Table | Typed keys |
|---|---|
| `harness.claude` | `default_mode` (permission mode, default `auto`), `skip_auto_permission_prompt`, `cleanup_period_days`, `disable_claude_ai_connectors`, `allow` (extra native allow rules) |
| `harness.copilot`, `harness.vscode` | none besides `settings` and `instructions` |
| `harness.codex` | `model`, `model_reasoning_effort`, `approval_policy`, `sandbox_mode` |
| `harness.pi` | `default_provider`, `default_model`, `default_thinking_level`, `packages` (extra pi packages) |
| `harness.antigravity` | `agent_mode`, `tool_permission`, `enable_terminal_sandbox`, `allow_non_workspace_access`, `allow` |

## `[os.<goos>]` and `[profiles.<name>]`

`os.darwin`, `os.linux` and `os.windows` take the same tables as a profile except the instruction lists: `autonomy`, `permissions`, `models`, `context`, `ui`, `notifications`, `voice`, `telemetry`, `lsp`, `plugins`, `mcp` and `harness`. A profile also takes `instructions` (the profile's layer list).

## Complete example

```toml
#:schema https://raw.githubusercontent.com/giocaizzi/agents/main/schema/config.schema.json
schema_version = 1
profile = "work"
harnesses = ["claude", "codex", "opencode"]

[autonomy]
ask_timeout = "5m"

[permissions]
allow_commands = ["git status", "make test"]
deny_read = ["**/.env"]
deny_command_prefixes = ["sudo"]
ask_command_prefixes = ["git push"]

[models]
default = "main"
default_effort_level = "high"

[models.tiers.main]
claude = "claude-sonnet-4-5"
codex = "gpt-5"
opencode = "anthropic/claude-sonnet-4-5"
effort = "medium"
[models.tiers.main.efforts]
codex = "high"

[models.tiers.fast]
claude = "claude-haiku-4-5"

[models.assign]
"acme/*" = "main"
"acme:reviewer" = "fast"

[ui]
theme = "dark"

[voice]
enabled = true
mode = "hold"

[telemetry]
enabled = true
endpoint = "${OTEL_ENDPOINT}"
protocol = "http/protobuf"

[lsp]
enabled = true
[lsp.servers.gopls]
command = ["gopls"]
format = true
[lsp.servers.gopls.extensions]
".go" = "go"

[instructions]
edit_command = ["code", "{file}"]
[instructions.vars]
team = "acme"
[instructions.layers.core]
file = "core.md"
[instructions.layers.work]

[plugins]
registry = "acme/plugins"
[plugins.enabled]
review = ["claude", "codex"]
[plugins.standalone.lint]
source = "acme/lint"
harnesses = ["claude"]

[mcp.servers.docs]
url = "https://mcp.example.com/mcp"
headers = { Authorization = "Bearer ${DOCS_TOKEN}" }
[mcp.servers.docs.oauth]
client_id = "client-id"
callback_port = 8765
public_client = true

[mcp.workspaces.app]
path = "~/src/app"
[mcp.workspaces.app.servers.db]
command = "npx"
args = ["-y", "example-db-server"]

[harness.claude]
default_mode = "auto"
instructions = ["core"]
[harness.claude.settings]
language = "english"

[os.windows]
[os.windows.permissions]
deny_command_prefixes = ["sudo", "runas"]

[profiles.work]
instructions = ["core", "work"]
[profiles.work.harness.codex]
instructions = ["core"]
[profiles.work.models]
default = "fast"
```
