<div align="center">

# agents

**Universal AI Agent Harness Manager.**  
Parity compiler, layered prompt engine, workspace MCP synchronizer, live multi-provider quota tracking, and health diagnostics.

[![Release](https://img.shields.io/github/v/release/giocaizzi/agents?sort=semver)](https://github.com/giocaizzi/agents/releases/latest)
[![CI](https://github.com/giocaizzi/agents/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/giocaizzi/agents/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

```text
┌────────────────────────────────────────────────────────────────────────┐
│  🔴 🟡 🟢  ~ / projects / app                                          │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│   $ agents usage -live                                                 │
│                                                                        │
│   PROVIDER     ACCOUNT         STATUS     QUOTA     TIER               │
│   Anthropic    personal@...    active     $42.50    Tier 4             │
│   OpenAI       work@...        active     $15.20    Tier 2             │
│   Google       personal@...    active     Free      Gemini 2.5         │
│   GitHub       giocaizzi       active     Pro       Copilot            │
│                                                                        │
│   4 providers active · All credentials valid                           │
│                                                                        │
├────────────────────────────────────────────────────────────────────────┤
│  [ claude-3-7-sonnet] [󰚥 84% quota] [󰊢 feat/new-setup] [ ~/code]    │
└────────────────────────────────────────────────────────────────────────┘
```

</div>

Developers frequently switch between AI coding harnesses—**Claude Code, GitHub Copilot CLI, Google Antigravity, OpenAI Codex, OpenCode, and Pi**. Each requires its own configuration formats, permission settings, subagent models, and MCP wiring.

`agents` provides a single declarative source of truth (`agents.toml`), automatically compiling native configurations, tracking token quotas across providers, synchronizing MCP servers, and monitoring tool health.

---

## Highlights

- **Multi-Harness Parity**: Compile declarative security rules, command allowlists, LSP language servers, and subagent models into native JSON/TOML configurations for 6 harnesses.
- **Bounded Harness Clamping**: Intelligently adapts models for bounded harnesses like GitHub Copilot CLI where arbitrary tier models (e.g. Claude Opus) are unavailable.
- **Layered Prompt Engine**: Compose system prompts dynamically from modular instruction layers with support for custom profiles (`[profiles.work]`, `[profiles.default]`).
- **Workspace MCP Sync**: Keep workspace `.mcp.json` definitions synchronized across Codex, Pi, and Antigravity configs automatically.
- **Live Provider Usage & Quota**: Inspect real-time subscription usage, billing limits, and reset dates across Anthropic, OpenAI, GitHub Copilot, and Google Gemini.
- **Health Doctor**: Diagnose model drift against LMArena rankings, detect orphaned plugins, and audit missing system dependencies.
- **Sub-5ms Status Line**: Native Go status bar hook for Claude Code, Copilot CLI, tmux, and oh-my-posh.
- **Cross-Harness Plugin Management**: Matrix view comparing upstream vs. installed plugin versions with automated sync and prune operations.

---

## Quickstart

### 1. Install

```bash
# Via Go
go install github.com/giocaizzi/agents/cmd/agents@latest

# Prebuilt binary (macOS / Linux / Windows)
curl -fsSL https://raw.githubusercontent.com/giocaizzi/agents/main/install.sh | sh
```

### 2. Initialize

Run in your project root or home directory to detect installed harnesses and scaffold an initial configuration:

```bash
agents init
```

This generates `agents.toml`:

```toml
[agents]
harnesses = ["claude", "copilot", "antigravity", "codex", "opencode", "pi"]
theme     = "dark"

[models]
driver    = "claude-3-7-sonnet"
scout     = "gemini-2.5-flash"
routine   = "claude-3-5-haiku"
escalate  = "claude-3-opus"

[tools]
deny      = ["rm -rf", "sudo", "git push --force"]
allow     = ["git status", "git diff", "make", "cargo test", "uv run"]

[lsp]
pyright   = { command = "pyright-langserver", languages = ["python"] }
gopls     = { command = "gopls",              languages = ["go"] }
ts        = { command = "typescript-language-server", languages = ["typescript", "javascript"] }

[mcp]
filesystem = { command = "npx", args = ["-y", "@modelcontextprotocol/server-filesystem", "."] }
fetch      = { command = "uvx", args = ["mcp-server-fetch"] }

[profiles.default]
instructions = ["prompts/core.md", "prompts/principles.md"]

[plugins]
registry = "https://github.com/giocaizzi/harness"
enabled  = ["foundations", "python", "dev-tools"]
```

### 3. Verify & Apply

```bash
# Check parity across all installed harnesses
agents check

# Render client-native configuration
agents render -harness claude
agents render -harness copilot

# Sync workspace MCP servers
agents mcp sync

# Check account usage and quotas
agents usage -live

# Run health diagnostics
agents doctor
```

---

## Chezmoi Integration

`agents` operates as a standalone tool, but also functions as a first-class **chezmoi plugin**.

When installed as `chezmoi-agents` (or symlinked in `~/.local/bin`), chezmoi automatically routes `chezmoi agents ...` commands to the binary. When executed in a chezmoi directory, it automatically resolves templates and data from `.chezmoidata/agents.toml`.

---

## Commands Reference

| Command | Arguments | Description |
|---|---|---|
| `init` | `[-global] [-dir <path>]` | Probe installed harnesses and scaffold `agents.toml` |
| `statusline` | `[claude│copilot│tmux│omp]` | High-speed (<5ms) Catppuccin status bar output |
| `check` | `[-file <path>]` | Validate configuration, parity, and plugin health |
| `render` | `-harness <name>` | Compile native configuration JSON/TOML |
| `prompt` | `[-profile <name>]` | Compose layered instruction prompt |
| `mcp sync` | `[path]` | Synchronize workspace `.mcp.json` to harnesses |
| `models` | `[sync│render]` | Inspect LMArena ranks, Epoch ECI, and pricing |
| `usage` | `[-live]` | Live subscription usage per provider account |
| `doctor` | `[-live] [-q]` | Advisory on model capacity, drift, and accounts |
| `plugins` | `[list│update│prune]` | Cross-harness plugin lifecycle management |

---

## License

[MIT](LICENSE) © Giorgio Caizzi
