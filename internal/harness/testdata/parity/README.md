# Parity fixture

`config.toml` holds the settings a dotfiles repository used to render into each
harness's native files with templates, before agents applied them itself. `golden/`
holds what those templates produced from the same settings, per profile (`personal`,
`work`) and per starting point: `fresh` (no native file yet) and `live` (the file in
`live/` already present). Paths use the home directory `/home/user`, the host name
`host.example` and the architecture `arm64`; the environment is

| Variable | Value |
|---|---|
| `AGENTS_OTEL_ENDPOINT` | `https://otel.example.com` (personal profile only) |
| `MCP_ACCOUNT` | `acct` |
| `MCP_CLIENT_ID` | `client-123` |

The parity test applies the fixture and compares the result with the golden file:
JSON and TOML by value (key order is not compared), text byte for byte. The
differences agents makes on purpose are listed in the test, each with its reason.
The only edit to the captured output is the header line of
`golden/*/fresh/codex/rules/agents.rules`, which named the previous tool.
