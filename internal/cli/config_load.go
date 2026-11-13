package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/finding"
	"github.com/giocaizzi/agents/internal/harness"
)

// Exit codes shared by every command.
const (
	exitOK       = 0 // success
	exitFindings = 1 // findings, drift or a failed operation
	exitUsage    = 2 // bad flags or arguments
)

// stdinPath is the -file value that reads the config from stdin.
const stdinPath = "-"

// configFlags are the flags of every command that reads the configuration.
type configFlags struct {
	file, profile, harnesses string
}

// addConfigFlags registers -file, -profile and -harnesses on fs. It reads no
// environment: help must render without it.
func addConfigFlags(fs *flag.FlagSet) *configFlags {
	f := &configFlags{}
	fs.StringVar(&f.file, "file", "", "config file (default: $AGENTS_CONFIG, else the user config.toml); - reads stdin")
	fs.StringVar(&f.profile, "profile", "", "profile to select (overrides $AGENTS_PROFILE and the profile key)")
	fs.StringVar(&f.harnesses, "harnesses", "", "comma-separated harnesses to configure (overrides $AGENTS_HARNESSES)")
	return f
}

// loadConfig resolves the configuration for f.
func (d Deps) loadConfig(f *configFlags) (*config.Resolved, error) {
	opts := config.Options{
		Getenv:     d.Getenv,
		GOOS:       d.GOOS,
		Home:       d.home(),
		ConfigPath: f.file,
		Profile:    f.profile,
	}
	if f.harnesses != "" {
		opts.Harnesses = strings.Split(f.harnesses, ",")
	}
	if f.file == stdinPath {
		opts.ReadFile = func(p string) ([]byte, error) {
			if p == stdinPath {
				return io.ReadAll(d.Stdin)
			}
			return os.ReadFile(p) //nolint:gosec // config paths come from the user
		}
	}
	r, err := config.Load(opts)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return r, nil
}

// validate is config.Validate plus the checks that read the machine: expand_env
// is refused on a workspace inside a git work tree.
func (d Deps) validate(r *config.Resolved) []finding.Finding {
	return append(config.Validate(r.Config), config.ValidateWorkspaces(r.Config, d.home(), d.Getenv, os.Stat)...)
}

// home is the user home: $HOME ($USERPROFILE on Windows), else the OS answer,
// else "".
func (d Deps) home() string {
	key := "HOME"
	if d.GOOS == "windows" {
		key = "USERPROFILE"
	}
	if h := d.Getenv(key); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

// harnessEnv is the machine view the renderer needs for r.
func (d Deps) harnessEnv(r *config.Resolved) harness.Env {
	lookPath := d.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	hostname := d.Hostname
	if hostname == nil {
		hostname = os.Hostname
	}
	host, err := hostname()
	if err != nil {
		host = ""
	}
	host, _, _ = strings.Cut(host, ".")                                          // short name: macOS answers name.local
	agents, _ := harness.InstalledAgents(harness.RegistryDir(&r.Config, r.Dirs)) // a missing checkout lists none
	return harness.Env{
		Home:     d.home(),
		Hostname: host,
		Arch:     runtime.GOARCH,
		GOOS:     d.GOOS,
		Getenv:   d.Getenv,
		LookPath: lookPath,
		Dirs:     r.Dirs,
		Agents:   agents,
	}
}
