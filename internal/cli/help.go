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
	commandRender  = "render"
	commandApply   = "apply"
	commandCheck   = "check"
	commandConfig  = "config"
	commandSchema  = "schema"
	commandShow    = "show"
	commandPrompt  = "prompt"
	commandMCP     = "mcp"
	commandModels  = "models"
	commandPlugins = "plugins"
	commandVersion = "version"
	commandSync    = "sync"
	commandAdd     = "add"
	commandList    = "list"
	configHelpEnv  = "AGENTS_CONFIG: config file; AGENTS_PROFILE: profile; AGENTS_HARNESSES: harnesses; XDG_CONFIG_HOME (Windows APPDATA): config directory."
	pluginHelpEnv  = configHelpEnv + " XDG_DATA_HOME (Windows LOCALAPPDATA): registry checkouts."
)

// helpNode contains prose only. Options are rendered from the executing parser.
type helpNode struct {
	name, summary, arguments, defaults, env, example string
	children                                         []helpNode
}

func helpTree() helpNode {
	return helpNode{name: "", summary: "Configure every AI coding harness from one config file", children: []helpNode{
		{name: "init", summary: "Write a starter config.toml", defaults: "Writes the user config.toml with mode 0600 unless it exists; -force overwrites it, -print writes to stdout instead.", env: configHelpEnv, example: "agents init -print"},
		{name: commandCheck, summary: "Validate the configuration and the native files", defaults: "Validates the resolved config; -file - reads it from stdin; -live also reports native files that differ from what apply would write. Exits 1 on findings.", env: configHelpEnv, example: "agents check -live"},
		{name: commandConfig, summary: "Inspect the resolved configuration", children: []helpNode{
			{name: commandShow, summary: "Print the resolved configuration", defaults: "Prints TOML; -json prints JSON; -origin names the layer that set each key.", env: configHelpEnv, example: "agents config show -origin"},
		}},
		{name: commandSchema, summary: "Print the JSON Schema of the configuration", defaults: "Writes the schema generated from the running binary to stdout.", example: "agents schema"},
		{name: commandRender, summary: "Print the native files apply would write", defaults: "Prints each target path and the keys agents manages in it; -harness limits the output to one harness.", env: configHelpEnv, example: "agents render -harness claude"},
		{name: commandApply, summary: "Write the native files of every selected harness", defaults: "Merges only the keys agents manages into each file and records them in its state; -n prints the diff and writes nothing. Symlinked targets are refused.", env: configHelpEnv, example: "agents apply -n"},
		{name: commandPrompt, summary: "Edit, show and check instruction layers", children: []helpNode{
			{name: "edit", summary: "Edit one instruction layer", arguments: "<name>  Layer name.", defaults: "Runs instructions.edit_command ({name} and {file} are replaced), else $EDITOR.", env: configHelpEnv + " EDITOR: fallback editor.", example: "agents prompt edit core"},
			{name: commandShow, summary: "Print the composed instructions of one cell", defaults: "Uses the selected profile and the first selected harness unless -profile and -harness are set.", env: configHelpEnv, example: "agents prompt show -profile work -harness codex"},
			{name: "matrix", summary: "List the layers of every profile and harness", defaults: "Prints one row per profile and harness.", env: configHelpEnv, example: "agents prompt matrix"},
			{name: commandCheck, summary: "Render every cell and report problems", defaults: "A missing layer file of another profile is reported as skipped; -all makes it an error.", env: configHelpEnv, example: "agents prompt check -all"},
		}},
		{name: commandMCP, summary: "Manage MCP servers of workspaces", children: []helpNode{
			{name: commandSync, summary: "Write the MCP files of workspaces", arguments: "[path]  Directory with a .mcp.json source (default: the configured workspaces).", defaults: "Without path, writes the MCP files of every configured workspace; with path, writes that directory's native MCP files from its .mcp.json. -n prints the diff and writes nothing.", env: configHelpEnv + " MCP_CONNECTOR_ACCOUNT: fallback for -account.", example: "agents mcp sync -n"},
			{name: commandAdd, summary: "Add or update a workspace MCP server and sync", arguments: "<name> <commandOrUrl> [args...]  Server name, executable or URL, and forwarded server arguments.", defaults: "Transport is inferred from the command or URL. Options must precede name; following arguments are forwarded unchanged.", env: "MCP_CONNECTOR_ACCOUNT: fallback for -account when synchronizing.", example: "agents mcp add -e MODE=local example npx -y example-server"},
		}},
		{name: commandModels, summary: "Display model tiers (default) or sync data", defaults: "Without a subcommand, displays the model tier list from the model catalog cache.", env: configHelpEnv + " XDG_CACHE_HOME (Windows LOCALAPPDATA): model catalog cache.", example: "agents models", children: []helpNode{
			{name: commandSync, summary: "Refresh model data and recompute tiers", defaults: "Fetches live model and benchmark data and writes the model catalog cache.", env: configHelpEnv + " XDG_CACHE_HOME (Windows LOCALAPPDATA): model catalog cache.", example: "agents models sync"},
		}},
		{name: commandPlugins, summary: "Inspect, update, or prune plugins", defaults: "Without a subcommand, lists plugins.", children: []helpNode{
			{name: commandList, summary: "Inspect configured plugins", defaults: "Reads plugins from the configuration and the registry checkout.", env: pluginHelpEnv, example: "agents plugins list -json"},
			{name: "update", summary: "Update plugins across harnesses", defaults: "Pulls the registry checkout at most once a day (-force always); -n previews changes.", env: pluginHelpEnv, example: "agents plugins update -n"},
			{name: "prune", summary: "Prune plugins across harnesses", defaults: "-n previews removals.", env: pluginHelpEnv, example: "agents plugins prune -n"},
		}},
		{name: "usage", summary: "Show live provider account usage and refresh the cache", defaults: "Queries all discovered providers unless -provider is set.", env: "HOME, XDG_CONFIG_HOME: account discovery; XDG_CACHE_HOME: usage cache; provider credentials are discovered from native client stores and provider environment variables.", example: "agents usage -provider anthropic"},
		{name: "doctor", summary: "Advise on models, accounts, and capacity from cached usage", defaults: "Reads configuration and the usage cache without refreshing provider usage.", env: configHelpEnv + " XDG_CACHE_HOME: usage cache.", example: "agents doctor -json"},
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
	if len(n.children) > 0 || name == "" || name == helpCmd || name == commandVersion {
		d.printHelp(d.Stdout, name, nil)
		return 0
	}
	// Use the execution parser itself, so help cannot drift from accepted flags.
	return d.Run(ctx, append(append([]string{}, path...), "--help"))
}
