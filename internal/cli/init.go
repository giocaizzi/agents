package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/term"
)

// starterConfig is the starter file; the verbs are the schema id and the
// harnesses array.
const starterConfig = `#:schema %s
schema_version = 1
harnesses = %s

# Everything below is an example: uncomment and adapt.

# Select a profile (a machine fact) and give it instruction layers.
# profile = "work"
# [profiles.work]
# instructions = ["core"]

# Instruction layers are files in the instructions directory.
# [instructions.layers.core]
# file = "core.md"

# Model tiers hold one native model id per harness; assign tiers to agents.
# [models]
# default = "main"
# [models.tiers.main]
# claude = "claude-sonnet-4-5"
# [models.assign]
# "acme/*" = "main"

# MCP servers: exactly one of url or command.
# [mcp.servers.docs]
# url = "https://mcp.example.com/mcp"
`

func (d Deps) runInit(_ context.Context, args []string) int {
	flags := d.newFlags("init")
	cf := addConfigFlags(flags)
	force := flags.Bool("force", false, "overwrite an existing config file")
	printOnly := flags.Bool("print", false, "write the starter file to stdout and touch nothing")
	if err := flags.Parse(args); err != nil {
		return d.parseFailure(flags, err)
	}
	st := term.NewStyle(d.Stdout, d.Getenv)

	dirs := config.ResolveDirs(d.Getenv, d.GOOS, d.home())
	target := d.Getenv("AGENTS_CONFIG")
	if cf.file != "" && cf.file != stdinPath {
		target = cf.file
	}
	if target == "" {
		target = filepath.Join(dirs.Config, "config.toml")
	}

	if !*printOnly && !*force {
		if _, err := os.Stat(target); err == nil {
			fmt.Fprintf(d.Stdout, "%s already exists; use -force to overwrite it\n", target)
			return exitOK
		}
	}

	hs, err := d.detectHarnesses(target)
	if err != nil {
		fmt.Fprintf(d.Stderr, "%s%v%s\n", st.R, err, st.O)
		return exitFindings
	}
	content := starterContent(hs)
	if *printOnly {
		fmt.Fprint(d.Stdout, content)
		return exitOK
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		fmt.Fprintf(d.Stderr, "%screate directory: %v%s\n", st.R, err, st.O)
		return exitFindings
	}
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		fmt.Fprintf(d.Stderr, "%swrite %s: %v%s\n", st.R, target, err, st.O)
		return exitFindings
	}
	fmt.Fprintf(d.Stdout, "%s✓%s Detected harnesses: %s%s%s\n", st.B, st.O, st.M, strings.Join(hs, ", "), st.O)
	fmt.Fprintf(d.Stdout, "%s✓%s Wrote %s%s%s\n", st.B, st.O, st.M, target, st.O)
	return exitOK
}

// detectHarnesses lists the harnesses whose home exists, ignoring any config
// file at target so that -force and -print detect afresh.
func (d Deps) detectHarnesses(target string) ([]string, error) {
	r, err := config.Load(config.Options{
		Getenv: func(k string) string {
			switch k {
			case "AGENTS_CONFIG", "AGENTS_HARNESSES", "AGENTS_PROFILE":
				return ""
			}
			return d.Getenv(k)
		},
		GOOS: d.GOOS,
		Home: d.home(),
		Stat: func(p string) (fs.FileInfo, error) {
			if p == target {
				return nil, fs.ErrNotExist
			}
			return os.Stat(p)
		},
		ReadFile: func(p string) ([]byte, error) {
			if p == target {
				return nil, fs.ErrNotExist
			}
			return os.ReadFile(p) //nolint:gosec // config paths come from the user
		},
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("detect harnesses: %w", err)
	}
	out := make([]string, 0, len(r.Harnesses))
	for _, h := range r.Harnesses {
		out = append(out, string(h))
	}
	return out, nil
}

func starterContent(hs []string) string {
	quoted := make([]string, 0, len(hs))
	for _, h := range hs {
		quoted = append(quoted, strconv.Quote(h))
	}
	return fmt.Sprintf(starterConfig, config.SchemaID, "["+strings.Join(quoted, ", ")+"]")
}
