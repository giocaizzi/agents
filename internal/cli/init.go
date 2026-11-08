package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/giocaizzi/agents/internal/term"
)

const initialConfigTemplate = `# agents.toml — Universal AI Agent Harness Configuration

[agents]
harnesses = %s
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
registry = "https://github.com/example/harness"
enabled  = ["core", "dev-tools"]
`

func (d Deps) runInit(_ context.Context, args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(d.Stderr)
	global := fs.Bool("global", false, "Write config to ~/.config/agents/agents.toml")
	dirFlag := fs.String("dir", "", "Target directory to write agents.toml")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	st := term.NewStyle(d.Stdout, d.Getenv)
	b := st.B
	m := st.M
	o := st.O

	detected := detectInstalledHarnesses()
	harnessList := `["claude", "copilot", "antigravity", "codex", "opencode", "pi"]`
	if len(detected) > 0 {
		var quoted []string
		for _, h := range detected {
			quoted = append(quoted, fmt.Sprintf("%q", h))
		}
		harnessList = fmt.Sprintf("[%s]", strings.Join(quoted, ", "))
	}

	targetPath := "agents.toml"
	if *dirFlag != "" {
		targetPath = filepath.Clean(filepath.Join(*dirFlag, "agents.toml"))
	} else if *global {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(d.Stderr, "%scannot find home directory: %v%s\n", st.R, err, o)
			return 1
		}
		dir := filepath.Clean(filepath.Join(home, ".config", "agents"))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			fmt.Fprintf(d.Stderr, "%scannot create directory %s: %v%s\n", st.R, dir, err, o)
			return 1
		}
		targetPath = filepath.Join(dir, "agents.toml")
	}

	if _, err := os.Stat(targetPath); err == nil {
		fmt.Fprintf(d.Stderr, "%sconfig file already exists at %s%s\n", st.Y, targetPath, o)
		return 0
	}

	content := fmt.Sprintf(initialConfigTemplate, harnessList)
	if err := os.WriteFile(targetPath, []byte(content), 0o600); err != nil {
		fmt.Fprintf(d.Stderr, "%sfailed to write %s: %v%s\n", st.R, targetPath, err, o)
		return 1
	}

	fmt.Fprintf(d.Stdout, "%s✓%s Detected harnesses: %s%s%s\n", b, o, m, strings.Join(detected, ", "), o)
	fmt.Fprintf(d.Stdout, "%s✓%s Initialized configuration: %s%s%s\n", b, o, m, targetPath, o)
	return 0
}

func detectInstalledHarnesses() []string {
	var detected []string
	home, err := os.UserHomeDir()
	if err != nil {
		return detected
	}
	checks := []struct {
		name string
		path string
	}{
		{harnessClaude, filepath.Join(home, ".claude")},
		{harnessCopilot, filepath.Join(home, ".copilot")},
		{harnessAntigravity, filepath.Join(home, ".gemini", "antigravity-cli")},
		{harnessCodex, filepath.Join(home, ".codex")},
		{harnessOpencode, filepath.Join(home, ".opencode")},
		{harnessPi, filepath.Join(home, ".pi")},
	}
	for _, c := range checks {
		if _, err := os.Stat(c.path); err == nil {
			detected = append(detected, c.name)
		}
	}
	return detected
}
