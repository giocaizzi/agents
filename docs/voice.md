# Voice input parity

The policy's `[voice]` table (`enabled`, `mode` = `hold` or `tap`, `autoSubmit`, `language` as a BCP 47 tag) is one dictation preference for every harness. `agents` maps a field only where the harness documents a settings key for it. Everything else is manual and listed below.

How the mapping reaches a harness:

- **Translate**: `internal/translate/<harness>.go` turns the policy into the harness's native settings. `agents render -harness <name>` prints that output.
- **Write**: `agents` writes no harness settings file itself. Your config manager does that. In a chezmoi source, for example, the templates `.chezmoitemplates/claude-settings.json` and `.chezmoitemplates/copilot-settings.json` read the policy, and the VS Code `settings.json` is a static file.
- **Check**: `agents check` runs `internal/parity/files.go` on the chezmoi source. It fails when the Claude or Copilot template stops reading a mapped field from the policy, or when the static VS Code file disagrees with the policy.

## Matrix

| Harness | Native dictation | Where audio goes | `enabled` | `mode` | `autoSubmit` | `language` | Applied by | Drift check | Speech model and runtime | Manual step |
|---|---|---|---|---|---|---|---|---|---|---|
| Claude Code | Yes: `/voice`; hold Space | Cloud (vendor service); claude.ai sign-in only | `voice.enabled` | `voice.mode` | `voice.autoSubmit` | Not mapped: the setting also changes the reply language | `translate/claude.go`; settings template | Template must read `enabled`, `mode` and `autoSubmit`; a `language` key fails the check | Vendor-hosted, none on disk | None |
| Copilot CLI | Yes: `/voice`; hold Space, or Ctrl+X V to toggle | Local: transcription runs on the machine | `voice.enabled` | No key | No key | No key: multilingual model by default, English and Spanish models selectable in `/voice` | `translate/copilot.go`; settings template | Template must read `enabled` | ONNX models on ONNX Runtime GenAI through Foundry Local, downloaded by Copilot; not shared | Run `/voice` once per machine to download the runtime and model |
| VS Code Copilot | Yes: chat dictation, Cmd/Ctrl+I | Local on desktop | `dictation.enabled` | No key | No key | `agents.voice.language`, written only when set | `translate/vscode.go`; static `settings.json` edited by hand | The static file must hold the policy's `enabled` and `language` | VS Code's own; not shared | None |
| Codex | No dictation: `/voice` starts a realtime spoken conversation (cloud), not dictation into the prompt; the desktop app has its own dictation | — | Not mapped (no CLI dictation) | — | — | — | — | — | — | Use the OS dictation |
| opencode | No dictation found | — | Not mapped (no dictation) | — | — | — | — | — | — | Use the OS dictation |
| pi | Not implemented | — | — | — | — | — | — | — | — | Use the OS dictation |
| Antigravity | Yes: `/voice dictate`, `/record` or F5; `/voice live` or F6 is a spoken conversation | Cloud: a transcription service; no speech model is stored on disk, only an audio encoder. Voice mode streams to Gemini Live | Not mapped: no documented settings key | — | — | — | — | — | Vendor-hosted, none on disk | `/logout` and sign in once to grant the dictation permission; not for business or enterprise accounts |

## Shared speech model

No speech model is shared. Copilot CLI (ONNX models on its own Foundry Local runtime) and VS Code transcribe locally, each with its own model. Claude Code and Antigravity transcribe in the cloud.

Facts were checked against each harness's documentation and its installed CLI on 2026-11-11.
