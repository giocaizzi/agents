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
- Every native file is a target of the apply engine (`internal/apply`). The engine merges only the keys `agents` manages into the live file, records them in `<state>/apply.json`, keeps the live key order (new keys are appended), does not rewrite an unchanged file, refuses symlinked targets and supports a dry run. A file an application also writes (settings, MCP lists) therefore keeps everything `agents` does not own, and a key that agents stops managing is removed on the next run.

### Model ownership split

- **Plugins own** agent roles (the agent files) and Claude's frontmatter model default.
- **The agents config owns** free-named tiers (a native model id per harness, an effort, optional per-harness efforts), `models.default` (the tier of the main session model) and `models.assign`, the only join between agent ids or globs and tiers.
- `agents` never edits plugin files. A per-agent model is written only through a harness's own user-level setting; where a harness has none, the plugin's own model applies and `agents models tiers` says so. See [parity.md](parity.md#models) for the table.
- `check` is strict about the config (an unknown tier is an error) and lenient about the installed set (an assignment matching no installed agent is a warning; one a harness cannot honour is an info). `models` and `doctor` are advisory.

## Consequences

- A machine without a dotfiles manager works with `agents init`, an editor and `agents apply`.
- A new harness or key is a typed field plus a renderer and a test; there is no second list to update in another repository.
- Secrets never enter the config: `${VAR}` references expand at apply time and an unset one skips the entry with a warning. OAuth client secrets live only in the harness's keyring.
- The dotfiles repository can stay public: it carries machine facts, while the private instruction layer is deployed encrypted and read by `agents` as plain files.
- `agents` owns more files than before, so a symlinked target (a file the dotfiles manager links into place) is refused rather than written through. The owner of the link and the owner of the content must agree; see the recovery steps in [cli.md](cli.md#recover-from-drift-or-a-refused-symlink).
