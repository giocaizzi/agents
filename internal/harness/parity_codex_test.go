package harness

import (
	"fmt"
	"strings"
)

// adjustCodex edits a Codex golden (decoded) for the differences agents makes on
// purpose; every edit carries its reason.
func adjustCodex(c parityCase, file string, want any) any {
	switch file {
	case "config.toml":
		m, _ := want.(map[string]any)
		// The old templates never wrote the default effort or the plugin states;
		// agents renders models.default_effort_level and plugins.<name>@<registry>.enabled.
		m["model_reasoning_effort"] = "medium"
		plugins := map[string]any{}
		for _, name := range []string{"dev-tools", "foundations", "go", "guard", "infra", "javascript", "python", "sounds"} {
			plugins[name+"@giocaizzi-harness"] = map[string]any{"enabled": c.Profile != "work" || name != "go"}
		}
		m["plugins"] = plugins
		return want
	case "rules/agents.rules":
		return codexParityRules()
	}
	return want
}

// codexParityRules is the rules file agents derives from the fixture. The old
// templates hard-coded prose for the lease-push rules and wrote only the
// command patterns; agents also renders allow_commands, deny_command_exact,
// deny_command_prefixes and ask_command_prefixes, with generic justifications.
// It leaves out the allow rules for git and gh, which overlap deny patterns
// Codex cannot express ("git push * HEAD:main", "gh pr merge * --admin*"),
// and the lease-push allow: the deny * spans words, so
// "git push --force-with-lease origin HEAD:main" matches both.
func codexParityRules() string {
	var sb strings.Builder
	sb.WriteString("# Managed by agents from permissions: do not edit.\n")
	rule := func(decision, justification string, words ...string) {
		quoted := make([]string, len(words))
		for i, w := range words {
			quoted[i] = fmt.Sprintf("%q", w)
		}
		fmt.Fprintf(&sb, "prefix_rule(\n    pattern = [%s],\n    decision = %q,\n    justification = %q,\n    match = [%q],\n)\n",
			strings.Join(quoted, ","), decision, justification, strings.Join(words, " "))
	}
	for _, cmd := range []string{"make", "uv", "jq", "npm", "cargo", "go", "terraform"} {
		rule("allow", "Allowed by the agents permissions.", cmd)
	}
	for _, branch := range []string{"main", "master"} {
		rule("forbidden", "Denied by the agents permissions.", "git", "push", "--force-with-lease", "origin", branch)
	}
	for _, words := range [][]string{{"env"}, {"printenv"}, {"pipx"}, {"pip"}, {"pip3"}, {"python", "-m", "pip"}, {"python3", "-m", "pip"}} {
		rule("forbidden", "Denied by the agents permissions.", words...)
	}
	rule("prompt", "Confirmation required by the agents permissions.", "bw")
	return sb.String()
}
