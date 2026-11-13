# CLI reference and runbook

`agents` configures every AI coding harness from one config file. This page lists the commands and the procedures to run them safely. Keys are in [config.md](config.md); the reasoning is in [design.md](design.md).

## Discover commands

```sh
agents                     # command list
agents help mcp add        # nested leaf: arguments, flags, defaults, example
agents apply --help        # same leaf help
```

Leaf help is built from the parser that runs the command, so it cannot drift from the accepted flags.

### Flags, streams and exit statuses

- Put options after the command path and before positional arguments: `agents apply -n`. Flags belong to their command; the only root aliases are `-h`/`--help` and `-v`/`--version`.
- Value flags accept `-file x` or `-file=x`. Booleans default to false and are given as `-n` or `-n=true`.
- Parsing stops at the first positional argument or `--`. For `mcp add`, options precede the server name; everything after the executable is forwarded to the server unchanged, `--help` included.
- Commands that load the config share `-file <path>` (`-` reads stdin), `-profile <name>` and `-harnesses a,b`. They override `AGENTS_CONFIG`, `AGENTS_PROFILE` and `AGENTS_HARNESSES`.

| Exit | Meaning |
|---|---|
| 0 | Success, or explicit help |
| 1 | Findings, drift, a refused target, or a failed operation |
| 2 | Usage error: unknown command, bad or missing flag, wrong argument count |

`models`, `doctor` and `usage` are advisory: they report and do not fail on a finding.

## Commands

### `init`

Writes a starter `config.toml` (mode 0600) in the config directory with the schema comment, `schema_version = 1` and the detected harnesses, plus commented examples. It does nothing and says so when the file exists.

| Flag | Meaning |
|---|---|
| `-force` | Overwrite an existing file |
| `-print` | Write the starter file to stdout and touch nothing |
| `-file` | Write to this path instead of the default |

### `check`

Validates the resolved config (schema version, harness names, declared layers and tiers, MCP server shape, OAuth values), the plugin registry and the model assignments, and renders every target to report warnings (skipped entries). A key set in the config that a selected harness ignores is reported once per key: info when the harness has no such setting, a warning when it has one that `agents` does not write yet ([feature matrix](features.md)). Findings are errors, warnings or info; only errors give exit 1.

| Flag | Meaning |
|---|---|
| `-live` | Also report native files that differ from what `apply` would write |
| `-json` | Print `{"findings": [...]}` with `where`, `message` and `severity` |

`agents check -file -` reads the config from stdin, so an edit step can check a rendered config before it is written.

### `config show`

Prints the resolved config as TOML. `-json` prints JSON; `-origin` appends the layer that set each key (`default`, a file path, `os darwin`, `profile work`, `env AGENTS_PROFILE`, `flag -profile`, `auto-detect`).

### `schema`

Prints the JSON Schema generated from the running binary. The committed copy is [`schema/config.schema.json`](../schema/config.schema.json); a test fails when they differ.

### `features`

Prints the [feature matrix](features.md): one row per config key, one column per harness, `✓` when `apply` writes the key, `-` when the harness has no such setting and `planned` when it has one that `agents` does not write yet. Reads no configuration.

| Flag | Meaning |
|---|---|
| `-harnesses` | Comma-separated harnesses to show (default: all) |
| `-json` | Print `{"harnesses": [...], "features": [...]}` with the target or evidence of every cell |

### `render`

Prints each target path and the keys `agents` manages in it, without writing anything. `-harness <name>` limits the output to one harness.

### `apply`

Writes the native files of every selected harness.

| Flag | Meaning |
|---|---|
| `-n` | Print the diff of each target and write nothing |

Each target is reported as `create`, `update`, `unchanged`, `delete` (a file `agents` made and no longer produces), `drift` (a check-only target that differs, or a retired text file you edited), `refused` (a symlink) or `error`. Warnings (a skipped `${VAR}`, a language server not on `PATH`) print to stderr.

How apply treats files:

- **State manifest.** `agents` records the keys it wrote in each file in `<state>/apply.json`. Only keys listed there are ever removed: a key you dropped from the config disappears from the live file on the next apply, a key `agents` never wrote is never touched. A file `agents` created and no longer produces is deleted, unless you edited it (then it is reported as `drift`, left in place and forgotten by the manifest). Losing the manifest loses removals, not your files.
- **One-time migration.** The first apply of a path that has no manifest entry also deletes the few keys an earlier tool wrote there (for example the local Claude marketplace entry of 0.2 and opencode `mcp.<name>` entries now written under `mcp.servers`). Every later deletion comes from the manifest.
- **Formats.** JSON, JSONC and TOML files are edited in place: comments, key order and unrelated formatting are preserved and new keys are appended. An unchanged file is not rewritten.
- **Diffs.** `-n` prints a unified diff in which values of keys `agents` does not manage show as `unmanaged` and every secret as `redacted`; a file that cannot be scanned prints "diff unavailable" rather than the raw text. The old value of a managed key removed from the config prints as `«removed»` on the `-` line that held it, whatever its path, because the manifest holds no values and the secret it held is no longer known.
- **Concurrent edits.** Each write is atomic and compares the file with what was read just before; if another program changed it meanwhile the write is refused with an error and nothing is overwritten. Re-run `agents apply`.
- **Permissions.** A new file is created with mode `0600`; an existing file keeps its mode, except one that receives expanded secrets (`expand_env`), which is tightened to `0600` before the write. Symlinked targets are refused. On Windows `0600` is not enforced: chmod only clears the read-only attribute, so a file holding secrets relies on the user profile's ACLs, and a read-only file is made writable and overwritten.

The files per harness are in [parity.md](parity.md#what-each-harness-receives). The VS Code `settings.json` is a check-only target: `agents` compares it and reports `drift` but never writes it.

### `prompt`

Instruction layers; see [Editing instructions](#editing-instructions).

| Command | Meaning |
|---|---|
| `prompt edit <name>` | Open one layer: `instructions.edit_command` with `{name}` and `{file}` replaced, else `$EDITOR` |
| `prompt show [-profile p] [-harness h]` | Print the composed text of one cell (default: the selected profile and the first selected harness) |
| `prompt matrix` | One row per profile and harness with its layer list |
| `prompt check [-all]` | Render every cell and report problems. A missing layer file of another profile is "skipped (not on this machine)"; `-all` makes it an error |

### `mcp`

| Command | Meaning |
|---|---|
| `mcp sync [-n] [-account id] [path]` | Without a path, write the MCP files of every configured workspace (`mcp.workspaces`). With a path, write that directory's native MCP files from its `.mcp.json` for the selected harnesses |
| `mcp add [-type t] [-env K=V] [-header 'H: V'] [-dir d] [-no-sync] [-account id] <name> <commandOrUrl> [args...]` | Add or update a server in `<dir>/.mcp.json`, then sync unless `-no-sync` |

A workspace with `expand_env = true` gets expanded values (plaintext, `0600`) for every harness; `mcp sync` refuses it before writing when the workspace is inside a git work tree.

`mcp sync` only touches the harnesses currently selected in `harnesses`. It does not remove the workspace file of a harness you dropped; delete that file by hand.

`-account` (default `$MCP_CONNECTOR_ACCOUNT`) fills the account placeholder of a `.mcp.json` source. Workspace files go through the same apply engine as every other target, so they get the same diffs, symlink refusal and merge rules. If the sync of `mcp add` fails, `.mcp.json` has already changed; fix the error and run `agents mcp sync <dir>`.

A `.mcp.json` source may declare a pre-registered OAuth client in Claude Code's style (`"oauth": {"clientId", "callbackPort"}`) or Copilot CLI's (`"oauthClientId"`, `"oauthPublicClient"`, `"auth": {"redirectPort"}`); per field the nested value wins. A client secret is never accepted or written. In `config.toml` the same client is `oauth.client_id`, `oauth.callback_port` and `oauth.public_client`.

#### OAuth redirect hosts

A provider that matches redirect URIs exactly needs the redirect each harness actually sends to be registered:

| Harness | Redirect URI |
|---|---|
| Claude Code | `http://localhost:<port>/callback` |
| Codex | `http://127.0.0.1:<port>/callback` |
| pi | `http://127.0.0.1:<port>/callback` |
| Antigravity | Fixed `https://antigravity.google/oauth-callback`; there is no port setting, so a configured port is left out with a warning |
| Copilot CLI | Not verified: `auth.redirectPort` is written, but it is not among the CLI's documented server fields |
| opencode | Not verified: only the client id is written |

Register both the `localhost` and the `127.0.0.1` form when one server is used from Claude Code and from Codex or pi. A confidential client (`public_client` false) gets its id written to Codex, pi and Antigravity without the secret; set the secret in each harness's own secret store, or the login fails at the token exchange.

### `plugins`

Bare `agents plugins` is `plugins list`. Plugins come from `[plugins]` in the config; the registry checkout is `<data>/registries/<repo>`.

| Command | Meaning |
|---|---|
| `plugins list [-json] [-harness h]` | Installed versus upstream versions per harness. `?` marks versions compared against the local checkout because the remote was unreachable |
| `plugins update [-n] [-force] [-harness h]` | Pull the checkout at most once every 24 hours (`-force` pulls always), then update stale plugins and install missing ones through each harness's own CLI |
| `plugins prune [-n]` | Remove superseded Claude and Codex cache versions and the obsolete Antigravity plugin |

Shared flags: `-harness-dir <path>` uses another checkout; `-n` and `-dry-run` are the same. A behind harness whose CLI is not on `PATH` is skipped with a warning. pi counts as installed when its `packages` entry resolves to the checkout, and `update` brings it current through the pull; opencode gets copies made by the checkout's installer, re-run by `update`. With the remote unreachable, `update` changes nothing and exits 1.

### `models`

| Command | Meaning |
|---|---|
| `models [-file models.json]` | Terminal tables of model tiers, costs and rankings from the catalog cache |
| `models tiers [-json]` | Matrix of what each tier gets per harness, then each assigned agent's tier and effective model per harness |
| `models sync [-file models.json]` | Fetch live model and benchmark data and write `<cache>/models.json` |

In `models tiers` a cell is the tier's model id, `plugin default` (the tier has no model for that harness), `session model` (written as the main session model) or `unsupported` (the harness has no per-agent setting, so the plugin's model applies).

### `usage` and `doctor`

`usage [-provider id]` queries every discovered provider account and rewrites the usage cache in `<cache>`; a provider that fails keeps its last values, marked stale. `doctor [-q] [-json]` reads the config and that cache only (no network) and advises on model drift, capacity and accounts; `-q` prints nothing when there is nothing to report.

### `version` and `help`

`agents version` (or `-v`) prints the build version, `dev` when unavailable. `agents help [command...]` shows help for a path.

## Runbooks

### First setup

**When:** a new machine, or the first use of `agents`.
**Preconditions:** the harnesses you use are installed and have been started once.

1. `agents init` writes the starter file. Use `agents init -print` to read it first.
2. Edit the file (path printed by `init`; keys in [config.md](config.md)). The `#:schema` line gives completion in editors.
3. `agents check` and fix errors.
4. `agents apply -n` and read the diff.
5. `agents apply`.

**Verify:** `agents check -live` prints `config ok`.
**Rollback:** there is no automatic undo and the previous content of a managed file is not kept. Restore it from your own backup or version control, or remove the keys from the config and apply again (managed keys that leave the config are removed from the live file).

### Everyday apply

Edit the config, then `agents check`, `agents apply -n`, `agents apply`. Another tool (a dotfiles repository, a provisioning script) can write `config.toml` and run a plain `agents apply`; nothing else couples them.

### Preview a change

`agents apply -n` prints the unified diff of each target and writes nothing; `agents render -harness claude` lists the managed keys of one harness. `agents config show -origin` shows which layer (user file, OS overlay, profile, environment, flag) set a value.

### Recover from drift or a refused symlink

- `agents check -live` lists native files that differ from what `apply` would write. Review each, then run `agents apply -n` to see what would change. Keys that `agents` does not manage are never touched, so a difference there is not drift.
- `refused <path>` means the target is a symlink. `agents` does not write through symlinks, because the file would change in the place the link points to. Replace the link with a regular file (move the content, remove the link, `agents apply`), or stop managing that file with the tool that created the link. The exit status stays 1 until it is resolved.
- A text file (an instruction file) reported as `drift` was written by `agents`, is no longer produced and was edited since. It is yours now and is not deleted; remove it yourself if you do not want it.
- A write refused because the file changed during the apply means another program was writing it; re-run `agents apply`.
- Never edit `<state>/apply.json` by hand. Deleting it is safe: the next apply treats every file as new, removes nothing it wrote earlier and repeats the one-time migration.
- A check-only target (the VS Code `settings.json`) reports `drift` when its managed keys differ. Edit the file yourself; `agents` will not.
- An `error` result names the file and the cause (unreadable file, invalid JSON, a layer failing to render). Fix it and re-run; targets that succeeded are already recorded.

### Editing instructions

Instruction layers are Markdown files in `instructions.dir` (default `<config dir>/instructions`), rendered with Go `text/template`. Template data: `.harness`, `.profile`, `.os`, `.hostname` and `.vars.<name>` (from `instructions.vars`); a missing key is an error. A cell (profile, harness) is the profile's list, then the harness's list, then the cell's own list, deduplicated keeping the first occurrence.

1. `agents prompt matrix` shows which layers each cell uses.
2. `agents prompt edit <layer>` opens the file.
3. `agents prompt check` renders every cell. A layer file that belongs only to another profile is reported as "skipped (not on this machine)" unless you pass `-all`.
4. `agents prompt show -profile work -harness codex` previews one cell.
5. `agents apply`.

The composed text is written whole to `~/.claude/CLAUDE.md`, `~/.copilot/copilot-instructions.md`, `~/.codex/AGENTS.md`, `~/.config/opencode/AGENTS.md`, `~/.pi/agent/AGENTS.md` and `~/.gemini/config/AGENTS.md`. An empty composition writes nothing.

### Inspect and update plugins

**When:** plugin versions are missing or stale, or caches need cleanup.
**Preconditions:** `plugins.registry` is set and the target harness CLIs are on `PATH`. `update` pulls the registry checkout.

1. `agents plugins list`.
2. `agents plugins update -n`, then `agents plugins update`.
3. `agents plugins prune -n`, then `agents plugins prune`.

**Verify:** `agents plugins list` shows no `↑`. A `?` means the upstream comparison was unavailable.
**Rollback:** none for updates or pruning; restore the checkout revision and reinstall with the harness's own tooling.

### Upgrading from 0.2

0.3 is a standalone tool. What changed and what to do:

1. **Config location.** The config is `$XDG_CONFIG_HOME/agents/config.toml` (`~/.config/agents/config.toml`; `%APPDATA%\agents\config.toml` on Windows). Run `agents init -print` for a starter, move your settings over, then `agents check`. `AGENTS_CONFIG` or `-file` points at another file.
2. **No project config.** A project-level `agents.toml` is no longer read. Move what it set into the user config (a profile or `[os.<goos>]` overlay if it is machine-specific).
3. **Registry checkout.** The plugin registry is now checked out at `<data>/registries/<owner>/<repo>` (set `plugins.registry = "owner/repo"`). Run `agents plugins update` once to create it; the old location is not read and can be removed.
4. **Tiers replace role-based models.** Declare tiers under `[models.tiers.<name>]` with one model id per harness, name the main one in `models.default` and map agents with `[models.assign]`. See [config.md](config.md#models).
5. **First apply.** Run `agents apply -n`, read the diff, then `agents apply`. It records the manifest, then removes the keys 0.2 left behind that 0.3 no longer writes (the one-time migration described under [`apply`](#apply)). Everything else in your native files is kept.
6. **Secrets in workspace files.** Workspace MCP files keep `${VAR}` references instead of expanded values; re-run `agents mcp sync` and commit nothing that holds a token.
