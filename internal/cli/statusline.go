package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/giocaizzi/agents/internal/cache"
	"github.com/giocaizzi/agents/internal/term"
	agentusage "github.com/giocaizzi/agents/internal/usage"
)

func (d Deps) runStatusline(ctx context.Context, args []string) int {
	target := "generic"
	if len(args) > 0 {
		target = args[0]
	}

	st := term.NewStyle(d.Stdout, d.Getenv)
	branch := gitBranch(ctx)
	dir := currentDir()
	model := d.activeModel()
	quota := d.activeQuota()

	segs := []string{
		fmt.Sprintf("%s[%s %s%s]%s", st.D, st.M, model, st.D, st.O),
	}
	if quota != "" {
		segs = append(segs, fmt.Sprintf("%s[%s󰚥 %s%s]%s", st.D, st.B, quota, st.D, st.O))
	}
	if branch != "" {
		segs = append(segs, fmt.Sprintf("%s[%s󰊢 %s%s]%s", st.D, st.M, branch, st.D, st.O))
	}
	if dir != "" {
		segs = append(segs, fmt.Sprintf("%s[%s %s%s]%s", st.D, st.D, dir, st.D, st.O))
	}

	switch target {
	case harnessClaude, harnessCopilot:
		fmt.Fprintf(d.Stdout, "%s\n", strings.Join(segs, " "))
	default:
		fmt.Fprintf(d.Stdout, "%s\n", strings.Join(segs, " "))
	}
	return 0
}

func (d Deps) activeModel() string {
	if p, _, err := d.loadPolicy(""); err == nil && p != nil && p.Models.Tiers != nil {
		if driver, ok := p.Models.Tiers["driver"]; ok && driver.Model != "" {
			return driver.Model
		}
	}
	return "agent"
}

func (d Deps) activeQuota() string {
	cacheDir, err := cache.Dir(d.Getenv)
	if err != nil {
		return ""
	}
	cacheFile := filepath.Clean(filepath.Join(cacheDir, "usage.json"))
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return ""
	}
	c, err := agentusage.ParseCache(data)
	if err != nil {
		return ""
	}
	for _, snap := range c.Snapshots {
		for _, lim := range snap.Limits {
			if lim.UsedPct != nil && *lim.UsedPct > 0 {
				return fmt.Sprintf("%d%%", int(*lim.UsedPct))
			}
		}
	}
	return ""
}

func gitBranch(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func currentDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(cwd, home) {
		return "~" + cwd[len(home):]
	}
	return filepath.Base(cwd)
}
