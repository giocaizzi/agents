# CLI reference and runbook

Use `agents` to discover commands, then ask for help at the level you need. The executable's leaf help reads options from the same parser that executes the command.

## Discover commands

```sh
agents                        # root command list
agents --help                 # same root help; -h also works
agents help mcp               # group navigation: sync and add
agents help mcp add           # nested leaf: arguments, flags, defaults, example
agents mcp add --help         # same leaf help; -h also works
agents help render            # settings and prompt rendering options
agents help models sync       # model catalog sync options
agents help plugins update    # plugin update options
```

Root help lists immediate commands, group help lists immediate children, and leaf help describes that command's arguments, options, defaults, environment and example. Groups do not aggregate every child's flags. `models` and `plugins` also have default actions: bare `agents models` displays tiers; bare `agents plugins` lists plugins. Bare `agents mcp` instead requires `sync` or `add`.

### Flag placement and parser boundaries

- Put command options **after the command path and before positional arguments**: `agents mcp sync -check .`.
- Flags belong to their command, not to a global option set. Root-only aliases are `agents -h` / `agents --help` for help and `agents -v` / `agents --version` for version. Use `agents version` for the named command. For example, use `agents render -file agents.toml`, not `agents -file agents.toml render`.
- Value flags accept `-file agents.toml` or `-file=agents.toml`; the parser also accepts double-dash spellings. Boolean flags default to false; use `-check` or `-check=true`, not `-check true`.
- Parsing stops at the first positional argument or `--`. A flag's value is data, even if it looks like a help token. Help is not intercepted by scanning the whole argument list.
- For `mcp add`, options must precede the server name. Arguments after the executable are forwarded unchanged, including `-h` or `--help`. For example, `agents mcp add -no-sync example example-server --help` stores `--help` as a server argument; it does not display `agents` help. Use `agents help mcp add` to inspect the command without adding a server.

### Streams and exit statuses

| Result | Stream and exit status |
|---|---|
| Explicit help (`help`, `-h`, `--help`) | Help on stdout, exit **0**, no command action |
| Unknown command/subcommand/help path | Diagnostic and relevant local guidance on stderr, exit **2** |
| Invalid flag or missing flag value | Diagnostic and command-local help on stderr, exit **2** |
| Missing required `mcp add` arguments | Diagnostic and `mcp add` help on stderr, exit **2** |
| Successful command | Normal output on stdout, exit **0** |
| Operational failure | Usually a diagnostic on stderr and exit **1**; see command-specific notes below |

An advisory or partial-data command need not fail for every warning: `doctor` findings are advisory, and `usage` may show stale cached data after a provider failure. Inspect output as well as the exit status.

## Commands reference

All commands support `-h` / `--help` at their option boundary. Unless stated otherwise, optional string flags default to empty (discovery or no filter), and boolean flags default to false.

### `init`

Detect installed harnesses and scaffold `agents.toml` in the current directory.

| Flag | Meaning/default |
|---|---|
| `-global` | Write `~/.config/agents/agents.toml` |
| `-dir <path>` | Select an existing target directory; takes precedence over `-global` |

```sh
agents init
agents init -global
```

### `check`

Validate policy, client parity and plugin registry configuration.

| Flag | Meaning/default |
|---|---|
| `-file <path>` | Explicit `agents.toml`, `harness-policy.toml` or `harness.toml`; otherwise discover it |
| `-profile <name>` | Apply a configuration profile |
| `-harness-dir <path>` | Registry checkout; defaults to `HARNESS_DIR`, then discovery |

```sh
agents check -file agents.toml
```

### `render`

Print native configuration or an effective harness prompt to stdout. This command does not write harness settings files; redirect output or use a configuration manager to apply it.

| Flag | Meaning/default |
|---|---|
| `-harness <name>` | Target: `claude`, `antigravity`, `copilot`, `codex`, `pi`, `opencode` |
| `-part <type>` | Defaults to settings; `permissions` selects Claude/Antigravity permissions; `prompt` selects the effective prompt. The parser also advertises `settings` and `all`; `all` currently follows the settings path, not a bundle of files |
| `-os <name>` | Target OS: `darwin`, `linux`, `windows`; defaults to the host OS |
| `-file <path>` | Explicit policy; otherwise discovery |
| `-profile <name>` | Apply a configuration profile |

There is no `render -target` flag. Codex settings output is TOML; other settings outputs are JSON.

```sh
agents render -harness pi -os linux
agents render -harness claude -part permissions -os windows
agents render -harness claude -part prompt
```

### `prompt`

Display the effective prompt, or check its presence without printing it. Resolves native harness instruction files first, then configured profile instructions or prompt layers.

| Flag | Meaning/default |
|---|---|
| `-harness <name>` | Optional target: `claude`, `antigravity`, `copilot`, `codex`, `pi`, `opencode` |
| `-profile <name>` | Prompt profile; default `default` |
| `-check` | Validate prompt presence/assembly instead of printing it |
| `-file <path>` | Optional explicit policy or layer file |

```sh
agents prompt -harness antigravity -check
agents prompt -profile work
```

### `mcp sync [path]`

Read workspace `.mcp.json` and generate native files for Codex (`.codex/config.toml`), Pi (`.pi/mcp.json`) and Antigravity (`.agents/mcp_config.json`). The workspace path defaults to `.`.

| Flag | Meaning/default |
|---|---|
| `-check` | Validate source and target preflight without writing |
| `-account <id>` | Placeholder account identifier; falls back to `MCP_CONNECTOR_ACCOUNT`. Check mode uses `example-account` if neither is set |

Preflight rejects symlink paths and existing targets not marked as generated by `agents mcp sync`.

An HTTP server may declare a pre-registered OAuth client in either style that shares `.mcp.json`: Claude Code's `"oauth": {"clientId", "callbackPort"}` or Copilot CLI's `"oauthClientId"`, `"oauthPublicClient"`, `"auth": {"redirectPort"}`. Per field the nested `oauth` value wins, else the Copilot one. Sync writes the client id and callback port to Codex and Pi, and the client id to Antigravity, which has no callback port setting: a port there is left out with a warning on stderr (its fixed redirect URI must be registered with the provider). A client secret is never accepted in `.mcp.json` nor written to a target. Other unknown keys still fail validation.

```sh
agents mcp sync -check .
agents mcp sync .
```

### `mcp add [options] <name> <commandOrUrl> [args...]`

Add or update a server in `.mcp.json`, then sync native targets unless `-no-sync` is set. An HTTP(S) URL infers HTTP transport; an executable infers stdio. HTTP servers do not accept trailing executable arguments.

| Flag | Meaning/default |
|---|---|
| `-type <stdio\|http>`, `-t` | Explicit transport; otherwise inferred |
| `-env KEY=value`, `-e` | Repeatable environment variable |
| `-header 'Header: Value'`, `-H` | Repeatable HTTP header |
| `-dir <path>` | Workspace directory; default `.` |
| `-no-sync` | Only update `.mcp.json` |
| `-account <id>` | Placeholder account identifier for sync; falls back to `MCP_CONNECTOR_ACCOUNT` |

```sh
agents mcp add -e MODE=local example npx -y example-server
agents mcp add -type http example https://mcp.example.com/mcp
```

If native sync fails after adding a server, `.mcp.json` has already changed. Resolve the sync error and rerun `agents mcp sync`.

### `models [sync]`

Bare `agents models` displays terminal tables of model tiers, costs and rankings; it accepts `-file <path>` for `models.json`.

| Command | Flags | Behavior |
|---|---|---|
| `models sync` | `-file <path>` | Fetch models.dev, LMArena and Epoch AI data, recompute tiers, and write only the JSON catalog |

The catalog is discovered when `-file` is omitted. Use `agents models` to inspect it in the terminal; use `agents render` separately to print native harness configuration.

```sh
agents models -file models.json
agents models sync -file models.json
```

### `plugins [list|update|prune]`

Bare `agents plugins` is `agents plugins list`. All three leaves accept these shared parser flags (not every flag changes every leaf's behavior):

| Flag | Meaning/default |
|---|---|
| `-file <path>` | Explicit `harness.toml`; otherwise discovery |
| `-harness-dir <path>` | Registry checkout; defaults to `HARNESS_DIR`, then discovery |
| `-harness <name>` | Narrow list/update to one harness; prune accepts this shared flag but does not filter candidates |
| `-n`, `-dry-run` | Preview update/prune without changing installations |
| `-f`, `-force` | Force mode for update |
| `-json` | JSON list output |

When no plugin directory is specified, automatic discovery prefers the managed installation at `~/.local/share/<registry-namespace>` before working-directory, `~/Documents`, and `~/src` developer checkouts. Pass `-harness-dir <path>` or set `HARNESS_DIR` to use a specific directory instead.

`agents plugins list` fetches the checkout's remote tip and reads upstream versions from it, without touching the working tree. When the remote is unreachable it falls back to the local checkout, marks those versions with `?` and warns that they may be out of date.

`agents plugins update` pulls the checkout, shows the same matrix and acts only on harnesses with a stale (`↑`) or missing plugin: it updates stale plugins, installs missing ones and skips the rest as up to date. A harness that is behind but whose CLI is not on `PATH` is skipped with a warning. Installs reported without a per-plugin version are never behind. `-harness <name>` narrows the run to one harness; `-n` prints the exact commands without running them. When the remote is unreachable it updates nothing and exits 1, as it does when a harness fails to update.

Two harnesses take plugins from the checkout itself instead of a marketplace:

- **pi** counts as installed only when a `packages` entry of `~/.pi/agent/settings.json` resolves to the checkout. It then runs whatever the checkout holds, so it shows the checkout's plugin versions, and `update` brings it current through the checkout pull (`by the checkout pull`); a missing link is installed with `pi install <checkout>`.
- **opencode** gets copies made by the checkout's `scripts/install.mjs opencode` in `$XDG_CONFIG_HOME/opencode` (default `~/.config/opencode`). A plugin whose copies match the checkout shows the checkout's version; copies that differ show `↑ outdated`, and `update` re-runs the script.

`agents plugins prune` removes superseded Claude/Codex cache versions and the obsolete Antigravity `api` plugin. Preview removals first with `-n`.

### `usage` and `doctor`

| Command | Flags/arguments | Behavior |
|---|---|---|
| `usage` | `-provider <id>` | Limit displayed output to one provider; refresh still queries all discovered providers |
| `doctor` | `-q`, `-json`, `-harness <comma-separated-list>` | Advisory model capacity, drift and account findings; quiet mode suppresses output when there is nothing to report |

`agents usage` always queries every provider and rewrites the usage cache; a provider that fails keeps its last cached values, marked stale with the error. `agents doctor` only reads that cache, so it stays offline with respect to provider usage. The cache uses `XDG_CACHE_HOME` (or the platform cache default).

```sh
agents usage -provider anthropic
agents doctor -json
```

### `version` and `help`

`agents version` prints build version information (`dev` when unavailable). Root aliases are `agents -v` and `agents --version`. `agents help [command...]` resolves a command path without executing its action.

## Runbook: inspect and update plugins

**When:** plugin versions are missing, stale or need cache cleanup.
**Preconditions:** an accessible registry checkout, configured `harness.toml`, and the target harness CLI on `PATH`. Review local checkout changes before updating; update pulls that checkout.

### Steps

1. Discover the command options: `agents help plugins update`.
2. Inspect state: `agents plugins list`.
3. Preview an update: `agents plugins update -n` (optionally add `-harness <name>`).
4. Apply the reviewed update: `agents plugins update`.
5. Preview cleanup: `agents plugins prune -n`.
6. Remove only the reviewed obsolete cache entries: `agents plugins prune`.

### Verify

Run `agents plugins list` again. Confirm the intended harnesses are installed and no longer show `↑`. A `?` means upstream comparison was unavailable, not that the installation is current. Use `agents check` separately to validate policy and registry configuration.

### Rollback

There is no automatic undo for plugin updates or pruning. Restore the previous checkout revision and reinstall the intended version using the harness's own tooling; pruned cache entries must be downloaded again. Do not treat dry-run output as a backup.

**Last verified:** 2026-11-11 against the CLI source and local binary for parser/help behavior; live plugin update/prune not executed (they modify installed harnesses).
