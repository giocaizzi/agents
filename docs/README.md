# Docs

Source of truth for decisions, architecture and runbooks of this repo.

- [Design](design.md): the decisions behind the tool: standalone config, one feature registry, manifest-only deletion, no project config, workspace secrets, model tiers.
- [Configuration reference](config.md): every config key, precedence, files and directories per OS, `${VAR}` expansion and where secrets can land, model tiers, a complete example.
- [CLI reference and runbook](cli.md): commands, flags, exit statuses, how apply treats files, and the procedures for first setup, everyday apply, previewing, recovering from drift or a refused symlink, editing instructions, plugin updates and upgrading from 0.2.
- [Harness parity matrix](parity.md): what `agents apply` writes per config key and harness, MCP OAuth clients, model tiers.
- [Voice input parity](voice.md): what `agents` maps, checks and leaves manual for dictation, per harness.
- [Feature matrix](features.md): which config keys each harness supports, where `agents` writes them, and what is planned (generated from `internal/feature`).
- [Release runbook](release.md): choosing the version and cutting a release with an annotated tag.
