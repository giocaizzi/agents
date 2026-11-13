# Voice input parity

The `[voice]` table (`enabled`, `mode` = `hold` or `tap`, `auto_submit`, `language` as a BCP 47 tag) is one dictation preference for every harness. `agents` maps a field only where the harness documents a settings key for it. Everything else is manual and listed below.

How the mapping reaches a harness:

- **Render**: `internal/harness/<harness>.go` turns the config into the harness's native settings. `agents render -harness <name>` lists the managed keys.
- **Write**: `agents apply` merges the keys into the harness's settings file. The VS Code `settings.json` is check-only: `agents apply` and `agents check -live` report a difference but never write the file.
- **Check**: `agents check -live` reports a native file that differs from what `apply` would write.

## Matrix

| Harness | Native dictation | Where audio goes | `enabled` | `mode` | `auto_submit` | `language` | Written by | Drift check | Speech model and runtime | Manual step |
|---|---|---|---|---|---|---|---|---|---|---|
| Claude Code | Yes: `/voice`; hold Space | Cloud (vendor service); claude.ai sign-in only | `voice.enabled` | `voice.mode` | `voice.auto_submit` | `voice.language`, written only when set; Claude Code also uses it as the reply language | `harness/claude.go`; `~/.claude/settings.json` | `check -live` reports a differing `voice` object | Vendor-hosted, none on disk | None |
| Copilot CLI | Yes: `/voice`; hold Space, or Ctrl+X V to toggle | Local: transcription runs on the machine | `voice.enabled` | No key | No key | No key: multilingual model by default, English and Spanish models selectable in `/voice` | `harness/copilot.go`; `~/.copilot/settings.json` | `check -live` reports a differing `voice.enabled` | ONNX models on ONNX Runtime GenAI through Foundry Local, downloaded by Copilot; not shared | Run `/voice` once per machine to download the runtime and model |
| VS Code Copilot | Yes: chat dictation, Cmd/Ctrl+I | Local on desktop | `dictation.enabled` | No key | No key | `agents.voice.language`, written only when set | `harness/vscode.go`; user `settings.json` (check-only, edited by hand) | `apply` reports `drift` when `dictation.enabled` or `agents.voice.language` differ | VS Code's own; not shared | None |
| Codex | No dictation: `/voice` starts a realtime spoken conversation (cloud), not dictation into the prompt; the desktop app has its own dictation | — | Not mapped (no CLI dictation) | — | — | — | — | — | — | Use the OS dictation |
| opencode | No dictation found | — | Not mapped (no dictation) | — | — | — | — | — | — | Use the OS dictation |
| pi | Not implemented | — | — | — | — | — | — | — | — | Use the OS dictation |
| Antigravity | Yes: `/voice dictate`, `/record` or F5; `/voice live` or F6 is a spoken conversation | Cloud: a transcription service; no speech model is stored on disk, only an audio encoder. Voice mode streams to Gemini Live | Not mapped: no documented settings key | — | — | — | — | — | Vendor-hosted, none on disk | `/logout` and sign in once to grant the dictation permission; not for business or enterprise accounts |

## Shared speech model

No speech model is shared. Copilot CLI (ONNX models on its own Foundry Local runtime) and VS Code transcribe locally, each with its own model. Claude Code and Antigravity transcribe in the cloud.

Facts were checked against each harness's documentation and its installed CLI on 2026-11-11.
