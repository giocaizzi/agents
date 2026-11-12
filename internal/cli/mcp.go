package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/giocaizzi/agents/internal/mcpsync"
	"github.com/giocaizzi/agents/internal/term"
)

type stringListFlag []string

func (s *stringListFlag) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringListFlag) Set(val string) error {
	*s = append(*s, val)
	return nil
}

func (d Deps) runMCP(ctx context.Context, args []string) int {
	if len(args) == 0 {
		st := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%smissing mcp subcommand (e.g. sync, add)%s\n\n", st.R, st.O)
		d.printHelp(d.Stderr, "mcp", nil)
		return 2
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "sync":
		return d.runMCPSync(ctx, rest)
	case "add":
		return d.runMCPAdd(ctx, rest)
	case "help", "--help", "-h":
		return d.explicitHelp(ctx, append([]string{"mcp"}, rest...))
	default:
		st := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%sunknown mcp subcommand: %s%s\n\n", st.R, sub, st.O)
		d.printHelp(d.Stderr, "mcp", nil)
		return 2
	}
}

func (d Deps) runMCPSync(_ context.Context, args []string) int {
	fs := d.newFlags("mcp sync")
	checkFlag := fs.Bool("check", false, "validate workspace MCP configuration without writing files")
	accountFlag := fs.String("account", "", "hostname account identifier; defaults to MCP_CONNECTOR_ACCOUNT")

	if err := fs.Parse(args); err != nil {
		return d.parseFailure(fs, err)
	}

	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}

	account := *accountFlag
	if account == "" {
		account = d.Getenv("MCP_CONNECTOR_ACCOUNT")
	}

	st := term.NewStyle(d.Stdout, d.Getenv)

	if *checkFlag {
		if account == "" {
			account = "example-account"
		}
		warnings, err := mcpsync.Check(root, account)
		if err != nil {
			errSt := term.NewStyle(d.Stderr, d.Getenv)
			fmt.Fprintf(d.Stderr, "%serror:%s %v\n", errSt.R, errSt.O, err)
			return 1
		}
		d.printMCPWarnings(warnings)
		srcFile := filepath.Join(root, mcpsync.SourceFilename)
		fmt.Fprintf(d.Stdout, "%s✓%s workspace MCP source is valid for Codex, pi and Antigravity (%s)\n", st.G, st.O, srcFile)
		return 0
	}

	outputs, warnings, err := mcpsync.Sync(root, account)
	if err != nil {
		errSt := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%serror:%s %v\n", errSt.R, errSt.O, err)
		return 1
	}
	d.printMCPWarnings(warnings)

	fmt.Fprintf(d.Stdout, "%s✓%s generated workspace MCP configuration for Codex, pi and Antigravity (%d files)\n", st.G, st.O, len(outputs))
	return 0
}

// printMCPWarnings reports, on stderr, settings a native target cannot represent.
func (d Deps) printMCPWarnings(warnings []string) {
	if len(warnings) == 0 {
		return
	}
	st := term.NewStyle(d.Stderr, d.Getenv)
	for _, w := range warnings {
		fmt.Fprintf(d.Stderr, "%swarning:%s %s\n", st.Y, st.O, w)
	}
}

func parseEnvFlags(envFlags []string) (map[string]string, error) {
	envMap := make(map[string]string)
	for _, e := range envFlags {
		k, v, found := strings.Cut(e, "=")
		if !found || k == "" {
			return nil, fmt.Errorf("invalid env format %q: expected KEY=value", e)
		}
		envMap[k] = v
	}
	return envMap, nil
}

func parseHeaderFlags(headerFlags []string) (map[string]string, error) {
	headerMap := make(map[string]string)
	for _, h := range headerFlags {
		k, v, found := strings.Cut(h, ":")
		if !found || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("invalid header format %q: expected 'Header: Value'", h)
		}
		headerMap[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return headerMap, nil
}

func buildAddOptions(name, cmdOrURL, typeFlag string, extraArgs []string, envMap, headerMap map[string]string) (mcpsync.AddOptions, error) {
	opts := mcpsync.AddOptions{
		Name:    name,
		Type:    typeFlag,
		Headers: headerMap,
		Env:     envMap,
	}

	switch typeFlag {
	case mcpsync.TransportHTTP:
		if len(extraArgs) > 0 {
			return opts, errors.New("unexpected extra arguments for HTTP server")
		}
		opts.URL = cmdOrURL
	case mcpsync.TransportStdio:
		opts.Command = cmdOrURL
		opts.Args = extraArgs
	default:
		if strings.HasPrefix(cmdOrURL, "http://") || strings.HasPrefix(cmdOrURL, "https://") {
			if len(extraArgs) > 0 {
				return opts, errors.New("unexpected extra arguments for HTTP server")
			}
			opts.Type = mcpsync.TransportHTTP
			opts.URL = cmdOrURL
		} else {
			opts.Type = mcpsync.TransportStdio
			opts.Command = cmdOrURL
			opts.Args = extraArgs
		}
	}

	return opts, nil
}

func (d Deps) runMCPAdd(_ context.Context, args []string) int {
	fs := d.newFlags("mcp add")
	typeFlag := fs.String("type", "", "transport type: stdio or http (auto-detected if omitted)")
	fs.StringVar(typeFlag, "t", "", "transport type alias")

	var envFlags stringListFlag
	fs.Var(&envFlags, "env", "environment variable in KEY=value format (repeatable)")
	fs.Var(&envFlags, "e", "env alias")

	var headerFlags stringListFlag
	fs.Var(&headerFlags, "header", "HTTP header in 'Header: Value' format (repeatable)")
	fs.Var(&headerFlags, "H", "header alias")

	dirFlag := fs.String("dir", ".", "workspace directory containing .mcp.json")
	noSyncFlag := fs.Bool("no-sync", false, "skip synchronizing native harness configuration files")
	accountFlag := fs.String("account", "", "account identifier for placeholder replacement")

	if err := fs.Parse(args); err != nil {
		return d.parseFailure(fs, err)
	}

	if fs.NArg() < 2 {
		st := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%smcp add requires <name> and <commandOrUrl>%s\n\n", st.R, st.O)
		d.printHelp(d.Stderr, "mcp add", fs)
		return 2
	}

	name := fs.Arg(0)
	cmdOrURL := fs.Arg(1)
	extraArgs := fs.Args()[2:]

	envMap, envErr := parseEnvFlags(envFlags)
	if envErr != nil {
		errSt := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%serror:%s %v\n", errSt.R, errSt.O, envErr)
		return 2
	}

	headerMap, hdrErr := parseHeaderFlags(headerFlags)
	if hdrErr != nil {
		errSt := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%serror:%s %v\n", errSt.R, errSt.O, hdrErr)
		return 2
	}

	opts, optErr := buildAddOptions(name, cmdOrURL, *typeFlag, extraArgs, envMap, headerMap)
	if optErr != nil {
		errSt := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%serror:%s %v\n", errSt.R, errSt.O, optErr)
		return 2
	}

	if addErr := mcpsync.AddServer(*dirFlag, opts); addErr != nil {
		errSt := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%serror:%s %v\n", errSt.R, errSt.O, addErr)
		return 1
	}

	st := term.NewStyle(d.Stdout, d.Getenv)
	if *noSyncFlag {
		fmt.Fprintf(d.Stdout, "%s✓%s added server %q to %s\n", st.G, st.O, name, mcpsync.SourceFilename)
		return 0
	}

	account := *accountFlag
	if account == "" {
		account = d.Getenv("MCP_CONNECTOR_ACCOUNT")
	}

	outputs, warnings, syncErr := mcpsync.Sync(*dirFlag, account)
	if syncErr != nil {
		errSt := term.NewStyle(d.Stderr, d.Getenv)
		fmt.Fprintf(d.Stderr, "%serror syncing native configuration:%s %v\n", errSt.R, errSt.O, syncErr)
		return 1
	}
	d.printMCPWarnings(warnings)

	fmt.Fprintf(d.Stdout, "%s✓%s added server %q to %s and synced native configurations (%d files)\n", st.G, st.O, name, mcpsync.SourceFilename, len(outputs))
	return 0
}
