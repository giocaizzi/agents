# Design: standalone config and the component split

Status: accepted.

## Context

`agents` first ran as a plugin of a dotfiles manager. The manager rendered harness settings from templates and `agents` only printed or checked them, so the knowledge of what each harness wants was split between templates and Go code, and `agents` could not run on a machine without that manager. Two problems followed: the harness settings were hard to verify (they were the output of templates, not of a typed model), and every other user had to adopt the same manager.

## Decision

### Three components, one owner each

| Component | Owns | Does not know |
|---|---|---|
| A dotfiles repository | Machine identity, secrets, encryption and deployment. It renders only machine facts (profile, harness selection, host-specific values) into `config.toml` and deploys the private instruction layer | What any harness's native file looks like |
| `agents` | Everything agent-related: the config model, the instruction layers, models and tiers, plugins, MCP, and the native files of every harness, which it writes itself | Any dotfiles manager. An architecture test (`internal/arch`) enforces it: no tracked file names one and no code reads its environment |
| A generic edit registry tool | Editing and checking components from a registry of names. For the agents config its check pipes the rendered config to `agents check -file -` and runs `agents prompt check` | What the components mean |

The dotfiles call plain `agents apply` after package installs. Nothing else couples them: the config file and the instruction directory are the whole interface.

### One typed config, written by one engine

- One TOML file, `$XDG_CONFIG_HOME/agents/config.toml`, with a JSON Schema, strict decoding and the layering described in [config.md](config.md). Every harness reads the same model; per-harness differences live in `harness.<name>` and in the renderers.
- Every native file is a target of the apply engine (`internal/apply`). The engine merges only the keys `agents` manages into the live file and keeps comments and key order in JSON, JSONC and TOML; it does not rewrite an unchanged file, refuses symlinked targets, writes atomically with a compare-and-swap against concurrent edits, and supports a dry run. A file an application also writes (settings, MCP lists) therefore keeps everything `agents` does not own.

### Feature registry: one table for support

Which harness supports which key, where `agents` writes it and why it does not is one table, `internal/feature/table.go`. The schema descriptions, `agents check`, `agents features` and the generated [features.md](features.md) all read it. Tests make it the only list: completeness (every config key has a row and every row a key), coverage (every managed key a renderer writes is declared by a rendered cell of that harness) and drift (features.md and the schema are regenerated, never edited). Adding a key is a model field, a table row, a renderer and a regenerate; forgetting any one fails a test.

### Manifest-only deletion

`agents` records the keys it writes in `<state>/apply.json` and removes only keys listed there. A key `agents` never wrote is never deleted, whatever its name. The one exception is a short per-target legacy list of keys an earlier tool wrote, deleted once on the first apply of a path (no manifest entry yet), so an upgrade cleans up after itself without a standing deletion rule.

### Project config is not read

0.2 searched the working directory for an `agents.toml`. 0.3 does not: `agents apply` writes into the home directory, including permission rules and commands, so letting a cloned repository change those would let untrusted content widen an agent's permissions. Everything comes from the user's own file, `[os.<goos>]` and profile overlays, and the environment. Per-project MCP servers are an explicit user decision instead: `mcp.workspaces` in the user config.

### Workspace files keep references

Workspace MCP files are written inside repositories and usually committed, so they never hold an expanded `${VAR}`. The reference stays where the harness expands it itself; for a harness that does not, the server is skipped with a warning. User-level files get expanded values, created `0600`, and diffs redact them.

### Model ownership split

- **Plugins own** agent roles (the agent files) and Claude's frontmatter model default.
- **The agents config owns** free-named tiers (a native model id per harness, an effort, optional per-harness efforts), `models.default` (the tier of the main session model) and `models.assign`, the only join between agent ids or globs and tiers.
- **Tiers replace roles.** A tier is a free name for a kind of work; it holds no role semantics, so users define as many as they want and plugins keep owning what an agent is.
- `agents` never edits plugin files. A per-agent model is written only through a harness's own user-level setting; where a harness has none, the plugin's own model applies and `agents models tiers` says so. See [parity.md](parity.md#models) for the table.
- `check` is strict about the config (an unknown tier is an error) and lenient about the installed set (an assignment matching no installed agent is a warning; one a harness cannot honour is an info). `models` and `doctor` are advisory.

## Consequences

- Any machine works with `agents init`, an editor and `agents apply`; another tool may only render machine facts into the config and call `agents apply`.
- A new harness or key is a typed field, a feature row and a renderer; there is no second list to update in another repository.
- Secrets never enter the config: `${VAR}` references expand at apply time and an unset one skips the entry with a warning. OAuth client secrets live only in the harness's keyring.
- The dotfiles repository can stay public: it carries machine facts, while the private instruction layer is deployed encrypted and read by `agents` as plain files.
- `agents` owns more files than before, so a symlinked target (a file the dotfiles manager links into place) is refused rather than written through. The owner of the link and the owner of the content must agree; see the recovery steps in [cli.md](cli.md#recover-from-drift-or-a-refused-symlink).
