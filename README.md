<div align="center">

# agents

**One config file for every AI coding harness.**  
Write native settings, instructions, plugins, MCP servers and model tiers for Claude Code, Copilot CLI, VS Code, Codex, opencode, pi and Antigravity, and track provider usage.

[![Release](https://img.shields.io/github/v/release/giocaizzi/agents?sort=semver)](https://github.com/giocaizzi/agents/releases/latest)
[![CI](https://github.com/giocaizzi/agents/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/giocaizzi/agents/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

<p align="center">
  <img src="docs/assets/showcase.png" alt="agents showcase: multi-provider quota tracking and health diagnostics" width="880">
</p>

</div>

Each AI coding harness has its own settings format, permission syntax, instruction file, plugin mechanism and MCP wiring. `agents` is a standalone command-line tool: it reads one TOML file and writes the native settings, MCP servers, rules, instructions, plugins and model choices of every harness you use. It merges only the keys it manages, so the rest of each file stays yours, and it shows a diff before it writes anything.

## Highlights

- **One config**: permissions, models, UI, voice, telemetry, language servers, plugins and MCP servers in a single schema-checked file, layered by OS and profile. See the [configuration reference](docs/config.md).
- **Safe apply**: only the keys `agents` wrote are ever removed, comments and key order are kept, `agents apply -n` diffs (secrets redacted), symlinked targets are refused, unchanged files are left alone.
- **Layered instructions**: Markdown templates composed per profile and harness.
- **Model tiers**: free-named tiers with one model id per harness, assigned to agents; plugins keep owning the agent roles.
- **Workspace MCP sync**: write each project's native MCP files, with pre-registered OAuth clients.
- **Usage and health**: live provider quota (`agents usage`) and advice on model drift and capacity (`agents doctor`).

Supported harnesses: Claude Code, Copilot CLI, VS Code, Codex, opencode, pi and Antigravity. What each one receives is in the [feature matrix](docs/features.md).

## Install

Download the binary for macOS, Linux or Windows from the [latest release](https://github.com/giocaizzi/agents/releases/latest) and put it on your `PATH`. Or build it:

```sh
go install github.com/giocaizzi/agents/cmd/agents@latest   # into $(go env GOPATH)/bin
make install                                               # from a clone: ~/.local/bin/agents
```

## Quickstart

```sh
agents init          # write a starter config.toml (detects installed harnesses)
# edit ~/.config/agents/config.toml (the path init printed)
agents check         # validate the config
agents apply -n      # preview the diff, write nothing
agents apply         # write the native files
```

A minimal `config.toml`:

```toml
#:schema https://raw.githubusercontent.com/giocaizzi/agents/main/schema/config.schema.json
schema_version = 1
harnesses = ["claude", "codex"]

[permissions]
deny_command_prefixes = ["sudo"]

[models]
default = "main"
[models.tiers.main]
claude = "claude-sonnet-4-5"
codex = "gpt-5"

[mcp.servers.docs]
url = "https://mcp.example.com/mcp"
```

The config lives in `$XDG_CONFIG_HOME/agents/config.toml` (`%APPDATA%\agents` on Windows). Drop `harnesses` to auto-detect the installed harnesses.

## Commands

| Command | Does |
|---|---|
| `init` | write a starter config |
| `check` | validate the config (`-live` also compares the native files) |
| `config show` | print the resolved config and where each key came from |
| `render`, `apply` | print or write the native files (`apply -n` previews) |
| `prompt` | edit, show and check instruction layers |
| `mcp sync`, `mcp add` | write workspace MCP files |
| `plugins` | list, update and prune plugins |
| `models`, `usage`, `doctor` | model tiers and catalog, provider quota, advice |
| `features`, `schema` | the support matrix and the JSON Schema |

`agents help <command>` shows the arguments and flags of each; the runbooks are in [docs/cli.md](docs/cli.md).

## Documentation

- [Documentation index](docs/README.md)
- [CLI reference and runbooks](docs/cli.md): commands, first setup, previewing, recovering from drift, upgrading from 0.2
- [Configuration reference](docs/config.md): every key, precedence, directories
- [Feature matrix](docs/features.md): which key each harness supports
- [Design](docs/design.md): the decisions behind the tool

## License

[MIT](LICENSE) © Giorgio Caizzi
