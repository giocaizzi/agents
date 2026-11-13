# agents

Go CLI that writes the native files of every AI coding harness from one config file (`$XDG_CONFIG_HOME/agents/config.toml`). Users: [README.md](README.md). Design and reference: [docs/](docs/README.md).

## Package map

- `cmd/agents/`: entrypoint.
- `internal/config/`: the typed model (`model.go`, one field per key), layered loading (`load.go`), XDG directories (`dirs.go`), validation (`validate.go`) and the generated JSON Schema (`schema.go`, committed as `schema/config.schema.json`). Reference: [docs/config.md](docs/config.md).
- `internal/feature/`: the feature registry, the single source of which key each harness supports (`table.go`); generates `docs/features.md`. Matrix: [docs/features.md](docs/features.md), details: [docs/parity.md](docs/parity.md).
- `internal/harness/`: renders the resolved config into apply targets per harness (`claude.go`, `copilot.go`, ...), model tiers (`tiers.go`) and workspace MCP files.
- `internal/apply/`: the engine that merges managed keys into live files, keeps the state manifest, diffs and refuses symlinks.
- `internal/instructions/`: layered instruction templates (profile x harness cells).
- `internal/cli/`: commands and the help tree (`help.go`). Procedures: [docs/cli.md](docs/cli.md).
- `internal/mcpsync/`: `.mcp.json` parsing, `mcp add` and the native MCP file shapes.
- `internal/pluginreg/`, `internal/pluginmgmt/`: plugin registry checkout and per-harness plugin lifecycle.
- `internal/catalog/`, `internal/doctor/`, `internal/usage/`, `internal/provider/`: model catalog, advice, provider usage.
- `internal/finding/`, `internal/term/`: findings and terminal styling.
- `internal/arch/`: architecture tests (no dotfiles-manager references, inner packages do not import `cli`).

## Rules

- **No dotfiles-manager coupling.** `agents` knows nothing about any dotfiles manager: no package, test fixture, path or comment may name one or read its files. The architecture test in `internal/arch` enforces it; do not weaken it. The dotfiles side renders only machine facts into the config and calls plain `agents apply`. Rationale: [docs/design.md](docs/design.md).
- **Public repo.** No personal usernames, private hostnames, internal paths or proprietary repository names in code, tests, docs or templates. Use `acme/app`, `example/harness`, `octocat`.
- **Registries stay abstract.** Plugin sources are configuration (`[plugins]`), never hardcoded.
- **Releases** are manual tag pushes today; release-please is merged but dormant until activated: [docs/release.md](docs/release.md).
- **Change a command, flag or key, change its doc in the same commit**: [docs/cli.md](docs/cli.md), [docs/config.md](docs/config.md), [docs/parity.md](docs/parity.md).
- **Adding a config key**: a field in `internal/config/model.go`, a row in `internal/feature/table.go` (a cell per harness: rendered, unsupported or planned), the renderer in `internal/harness/`, then regenerate: `go test ./internal/config -run TestSchemaDrift -update` (schema) and `go test ./internal/feature -run Drift -update` (`docs/features.md`). The completeness, coverage and drift tests fail otherwise. Never edit `docs/features.md` or `schema/config.schema.json` by hand.
- **No project config.** Nothing reads an `agents.toml` from the working directory; do not add it back (trust: [docs/design.md](docs/design.md#project-config-is-not-read)).
- **Deletion only through the manifest.** The apply engine removes only keys it recorded in its state manifest; see [docs/design.md](docs/design.md#manifest-only-deletion).

## Make targets

- `make check`: lint and tests (`lint test`).
- `make lint`: tidy check, then `golangci-lint` for linux, darwin and windows.
- `make test`: `go test -race -shuffle=on`.
- `make check-fast`: tests without the race detector.
- `make build`: static binary into `bin/`.
- `make fmt`, `make vuln`, `make help`.

## Commits

Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `chore:`). Pull requests are squash-merged once CI passes.
