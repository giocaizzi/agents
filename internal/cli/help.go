package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/giocaizzi/agents/internal/term"
)

const (
	commandRender     = "render"
	commandPrompt     = "prompt"
	commandMCP        = "mcp"
	commandModels     = "models"
	commandPlugins    = "plugins"
	commandStatusline = "statusline"
	commandVersion    = "version"
	commandSync       = "sync"
	commandAdd        = "add"
	commandList       = "list"
	pluginHelpEnv     = "HARNESS_DIR: registry checkout; CHEZMOI_SOURCE_DIR, HOME: configuration discovery."
)

// helpNode contains prose only. Options are rendered from the executing parser.
type helpNode struct {
	name, summary, arguments, defaults, env, example string
	children                                         []helpNode
}

func helpTree() helpNode {
	return helpNode{name: "", summary: "Universal AI harness configuration and policy tools", children: []helpNode{
		{name: "init", summary: "Detect harnesses and scaffold agents.toml", defaults: "Writes agents.toml in the current directory; -global uses ~/.config/agents/agents.toml.", example: "agents init -global"},
		{name: "check", summary: "Validate policy, client parity, and plugins", defaults: "Discovers policy and harness configuration when -file is omitted.", env: "AGENTS_CONFIG, CHEZMOI_SOURCE_DIR: policy discovery; HARNESS_DIR: registry checkout; HOME: user configuration.", example: "agents check -file agents.toml"},
		{name: commandRender, summary: "Render client-native configuration or prompt", defaults: "-part defaults to settings; -os defaults to the host operating system. Output goes to stdout.", env: "AGENTS_CONFIG, CHEZMOI_SOURCE_DIR, HOME: configuration discovery.", example: "agents render -harness pi -os linux"},
		{name: commandPrompt, summary: "Display or validate the effective system prompt", defaults: "Uses the default profile and discovers configuration and prompt layers.", env: "AGENTS_CONFIG, CHEZMOI_SOURCE_DIR, HOME: configuration and prompt discovery.", example: "agents prompt -harness claude -check"},
		{name: commandMCP, summary: "Manage workspace MCP configuration", children: []helpNode{
			{name: commandSync, summary: "Sync workspace MCP configuration to native clients", arguments: "[path]  Workspace directory (default: current directory).", defaults: "Writes native client files unless -check is set. Options must precede path.", env: "MCP_CONNECTOR_ACCOUNT: fallback for -account.", example: "agents mcp sync -check ."},
			{name: commandAdd, summary: "Add or update a workspace MCP server and sync", arguments: "<name> <commandOrUrl> [args...]  Server name, executable or URL, and forwarded server arguments.", defaults: "Transport is inferred from the command or URL. Options must precede name; following arguments are forwarded unchanged.", env: "MCP_CONNECTOR_ACCOUNT: fallback for -account when synchronizing.", example: "agents mcp add -e MODE=local example npx -y example-server"},
		}},
		{name: commandModels, summary: "Display model tiers (default), sync data, or render docs", defaults: "Without a subcommand, displays the model tier list.", env: "AGENTS_CONFIG, CHEZMOI_SOURCE_DIR, HOME: catalog and configuration discovery.", example: "agents models -file models.json", children: []helpNode{
			{name: commandSync, summary: "Refresh model data and recompute tiers", defaults: "Discovers models.json and docs/MODELS.md; fetches live model and benchmark data.", env: "AGENTS_CONFIG, CHEZMOI_SOURCE_DIR, HOME: catalog discovery.", example: "agents models sync -file models.json"},
			{name: commandRender, summary: "Regenerate model documentation offline", defaults: "Discovers models.json and docs/MODELS.md; -check checks freshness without writing.", env: "AGENTS_CONFIG, CHEZMOI_SOURCE_DIR, HOME: catalog discovery.", example: "agents models render -check"},
		}},
		{name: commandPlugins, summary: "Inspect, update, or prune plugins", defaults: "Without a subcommand, lists plugins.", children: []helpNode{
			{name: commandList, summary: "Inspect configured plugins", defaults: "Discovers harness.toml; shared plugin flags and aliases are accepted.", env: pluginHelpEnv, example: "agents plugins list -json"},
			{name: "update", summary: "Update plugins across harnesses", defaults: "Discovers harness.toml; -n previews changes; -f forces updates.", env: pluginHelpEnv, example: "agents plugins update -n"},
			{name: "prune", summary: "Prune plugins across harnesses", defaults: "Discovers harness.toml; -n previews removals. Shared plugin flags and aliases are accepted.", env: pluginHelpEnv, example: "agents plugins prune -n"},
		}},
		{name: "usage", summary: "Show live provider account usage and refresh the cache", defaults: "Queries all discovered providers unless -provider is set.", env: "HOME, XDG_CONFIG_HOME: account discovery; XDG_CACHE_HOME: usage cache; provider credentials are discovered from native client stores and provider environment variables.", example: "agents usage -provider anthropic"},
		{name: "doctor", summary: "Advise on models, accounts, and capacity from cached usage", defaults: "Reads configuration and the usage cache without refreshing provider usage.", env: "AGENTS_CONFIG, CHEZMOI_SOURCE_DIR, HOME: configuration discovery; XDG_CACHE_HOME: usage cache.", example: "agents doctor -json"},
		{name: commandStatusline, summary: "Output a fast status line", arguments: "[claude|copilot|tmux|omp]  Output target, not a subcommand (default: generic).", defaults: "Reads the local policy, usage cache, and Git branch.", env: "AGENTS_CONFIG, CHEZMOI_SOURCE_DIR, HOME: policy discovery; XDG_CACHE_HOME: usage cache.", example: "agents statusline claude"},
		{name: commandVersion, summary: "Print version information", defaults: "Prints the build version, or dev when unavailable.", example: "agents version"},
		{name: helpCmd, summary: "Show help for a command path", arguments: "[command...]  Command and optional nested subcommand.", defaults: "Without arguments, shows root help.", example: "agents help mcp add"},
	}}
}

func findHelp(path string) (helpNode, bool) {
	n := helpTree()
	for part := range strings.FieldsSeq(path) {
		found := false
		for _, child := range n.children {
			if child.name == part {
				n = child
				found = true
				break
			}
		}
		if !found {
			return helpNode{}, false
		}
	}
	return n, true
}

func (d Deps) printHelp(w io.Writer, path string, fs *flag.FlagSet) {
	n, ok := findHelp(path)
	if !ok {
		return
	}
	s := term.NewStyle(w, d.Getenv)
	command := strings.TrimSpace("agents " + path)
	fmt.Fprintf(w, "%s%s%s — %s\n\n%sUSAGE%s\n  %s", s.B, command, s.O, n.summary, s.B, s.O, command)
	if len(n.children) > 0 {
		fmt.Fprint(w, " [command]")
	}
	if n.arguments != "" {
		fmt.Fprint(w, " "+strings.Split(n.arguments, "  ")[0])
	}
	if fs != nil || path == "" || len(n.children) == 0 {
		fmt.Fprint(w, " [options]")
	}
	fmt.Fprintln(w)
	if len(n.children) > 0 {
		fmt.Fprintf(w, "\n%sCOMMANDS%s\n", s.B, s.O)
		for _, child := range n.children {
			fmt.Fprintf(w, "  %s%-12s%s %s\n", s.M, child.name, s.O, child.summary)
		}
	}
	if n.arguments != "" {
		fmt.Fprintf(w, "\n%sARGUMENTS%s\n  %s\n", s.B, s.O, n.arguments)
	}
	// Root and groups deliberately don't aggregate leaf options or examples.
	if path == "" {
		fmt.Fprintf(w, "\n%sOPTIONS%s\n", s.B, s.O)
		fmt.Fprintln(w, "  -h, --help\n    Show this help and exit.")
		fmt.Fprintln(w, "  -v, --version\n    Print version information and exit.")
	}
	if len(n.children) == 0 {
		fmt.Fprintf(w, "\n%sOPTIONS%s\n", s.B, s.O)
		if fs != nil {
			old := fs.Output()
			fs.SetOutput(w)
			fs.PrintDefaults()
			fs.SetOutput(old)
		}
		fmt.Fprintln(w, "  -h, --help\n    Show this help and exit.")
	}
	if n.defaults != "" {
		fmt.Fprintf(w, "\n%sDEFAULTS%s\n  %s\n", s.B, s.O, n.defaults)
	}
	if n.env != "" {
		fmt.Fprintf(w, "\n%sENVIRONMENT%s\n  %s\n", s.B, s.O, n.env)
	}
	if len(n.children) == 0 && n.example != "" {
		fmt.Fprintf(w, "\n%sEXAMPLES%s\n  %s\n", s.B, s.O, n.example)
	}
	if path == "" || len(n.children) > 0 {
		fmt.Fprintln(w, "\nUse agents help <command> for command-specific help.")
	}
}

func (d Deps) newFlags(name string) *flag.FlagSet {
	fs := newFlags(name)
	// flag invokes Usage for both help and errors; parseFailure chooses the stream.
	fs.Usage = func() {}
	return fs
}

func (d Deps) parseFailure(fs *flag.FlagSet, err error) int {
	if errors.Is(err, flag.ErrHelp) {
		d.printHelp(d.Stdout, fs.Name(), fs)
		return 0
	}
	fmt.Fprintf(d.Stderr, "flag parse error: %v\n\n", err)
	d.printHelp(d.Stderr, fs.Name(), fs)
	return 2
}

func (d Deps) explicitHelp(ctx context.Context, path []string) int {
	name := strings.Join(path, " ")
	n, ok := findHelp(name)
	if !ok {
		local := ""
		for i := range path {
			candidate := strings.Join(path[:i+1], " ")
			if _, exists := findHelp(candidate); !exists {
				break
			}
			local = candidate
		}
		fmt.Fprintf(d.Stderr, "unknown help command: %s\n\n", name)
		d.printHelp(d.Stderr, local, nil)
		return 2
	}
	if len(n.children) > 0 || name == "" || name == helpCmd || name == commandVersion || name == commandStatusline {
		d.printHelp(d.Stdout, name, nil)
		return 0
	}
	// Use the execution parser itself, so help cannot drift from accepted flags.
	return d.Run(ctx, append(append([]string{}, path...), "--help"))
}
