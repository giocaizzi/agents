# agents

Universal AI Agent Harness Manager (`agents` standalone CLI and `chezmoi-agents` parity engine).

## Purpose & Architecture

`agents` provides a declarative, cross-client management tool for AI coding agents across multiple harnesses: Claude Code, GitHub Copilot CLI, Google Antigravity, OpenAI Codex, OpenCode, and Pi.

- `cmd/agents/` — Main entrypoint (dispatched as `agents` or `chezmoi-agents`).
- `internal/config/` — Declarative policy and harness configuration parser (`agents.toml`, `harness.toml`).
- `internal/parity/` — Client parity compiler and rule verification across 6 harnesses.
- `internal/translate/` — Translates declarative rules and settings into native client formats.
- `internal/mcpsync/` — Bidirectional workspace MCP synchronization (`.mcp.json`).
- `internal/usage/` — Multi-provider token quota and billing monitor (Anthropic, OpenAI, Google, GitHub).
- `internal/provider/` — Credential discovery and quota API adapters.
- `internal/catalog/` — Model catalog, LMArena ranks, Epoch AI benchmarks, and pricing index.
- `internal/doctor/` — Diagnostic engine for model drift, stale plugins, and account health.
- `internal/pluginreg/` — Multi-registry and standalone plugin declarations and validator.
- `internal/pluginmgmt/` — Cross-harness plugin lifecycle (inspect, update, prune).
- `internal/term/` — Sub-5ms status line formatter, Catppuccin styles, and table rendering.

## Principles & Public Repository Integrity

- **Strict Generality**: This is an open-source, universal tool meant for the entire developer community.
- **No Personal Configurations**: Never hardcode personal usernames, private machine hostnames, internal corporate paths, or proprietary repository names in code, tests, documentation, or templates.
- **Generic Placeholders**: Always use standard generic placeholders in examples and tests: `example/harness`, `octocat`, standard CLI tools (`git`, `make`, `uv`), and standard directories.
- **Domain Independence**: Harness registries and plugin sources must remain fully abstract and configurable (`[plugins.registries]`, `[plugins.standalone]`).

## Development

- `make check` — Run linter and tests with race detection (`check: lint test`).
- `make lint` — Run `golangci-lint run`.
- `make test` — Run unit and integration tests with `go test -race -shuffle=on ./...`.
- `make build` — Build cross-platform binaries into `bin/`.

## Conventional Commits

Commits must follow Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`, `test:`). Pull requests are auto-merged using squash commits once CI checks pass.
