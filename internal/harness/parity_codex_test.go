package harness

// adjustCodex edits a Codex golden (decoded) for the differences agents makes on
// purpose; every edit carries its reason.
func adjustCodex(_ parityCase, file string, want any) any {
	if file != "rules/agents.rules" {
		return want
	}
	// The old templates hard-coded prose for the lease-push rules; agents derives
	// generic lines from any pattern.
	return `# Managed by agents from permissions: do not edit.
prefix_rule(
    pattern = ["git","push","--force-with-lease","origin"],
    decision = "allow",
    justification = "Allowed by the agents permissions.",
    match = ["git push --force-with-lease origin"],
)
prefix_rule(
    pattern = ["git","push","--force-with-lease","origin","main"],
    decision = "forbidden",
    justification = "Denied by the agents permissions.",
    match = ["git push --force-with-lease origin main"],
)
prefix_rule(
    pattern = ["git","push","--force-with-lease","origin","master"],
    decision = "forbidden",
    justification = "Denied by the agents permissions.",
    match = ["git push --force-with-lease origin master"],
)
`
}
