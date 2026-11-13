# agents

Go CLI that writes the native files of every AI coding harness from one config file (`$XDG_CONFIG_HOME/agents/config.toml`). Users: [README.md](README.md). Design and reference: [docs/](docs/README.md).

## Package map

- `cmd/agents/`: entrypoint.
- `internal/config/`: the typed model (`model.go`, one field per key), layered loading (`load.go`), XDG directories (`dirs.go`), validation (`validate.go`) and the generated JSON Schema (`schema.go`, committed as `schema/config.schema.json`). Reference: [docs/config.md](docs/config.md).
- `internal/harness/`: renders the resolved config into apply targets per harness (`claude.go`, `copilot.go`, ...), model tiers (`tiers.go`) and workspace MCP files. Matrix: [docs/parity.md](docs/parity.md).
- `internal/apply/`: the engine that merges managed keys into live files, keeps the state manifest, diffs and refuses symlinks.
- `internal/instructions/`: layered instruction templates (profile x harness cells).
- `internal/cli/`: commands and the help tree (`help.go`). Procedures: [docs/cli.md](docs/cli.md).
- `internal/mcpsync/`: `.mcp.json` parsing, `mcp add` and the native MCP file shapes.
- `internal/pluginreg/`, `internal/pluginmgmt/`: plugin registry checkout and per-harness plugin lifecycle.
- `internal/catalog/`, `internal/doctor/`, `internal/usage/`, `internal/provider/`, `internal/cache/`: model catalog, advice, provider usage.
- `internal/finding/`, `internal/term/`: findings and terminal styling.

## Rules

- **No dotfiles-manager coupling.** `agents` knows nothing about any dotfiles manager: no package, test fixture, path or comment may name one or read its files. An architecture test enforces it; do not weaken it. The dotfiles side renders only machine facts into the config and calls plain `agents apply`. Rationale: [docs/design.md](docs/design.md).
- **Public repo.** No personal usernames, private hostnames, internal paths or proprietary repository names in code, tests, docs or templates. Use `acme/app`, `example/harness`, `octocat`.
- **Registries stay abstract.** Plugin sources are configuration (`[plugins]`), never hardcoded.
- **Change a command, flag or key, change its doc in the same commit**: [docs/cli.md](docs/cli.md), [docs/config.md](docs/config.md), [docs/parity.md](docs/parity.md). `internal/config/model.go` changes need `go test ./internal/config -run TestSchemaDrift -update`.

## Make targets

- `make check`: lint and tests (`lint test`).
- `make lint`: tidy check, then `golangci-lint` for linux, darwin and windows.
- `make test`: `go test -race -shuffle=on`.
- `make check-fast`: tests without the race detector.
- `make build`: static binary into `bin/`.
- `make fmt`, `make vuln`, `make help`.

## Commits

Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `chore:`). Pull requests are squash-merged once CI passes.
