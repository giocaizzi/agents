// Package cache locates the agents cache directory, shared by every
// subcommand that caches data (usage.json, ...).
package cache

import (
	"fmt"
	"os"
	"path/filepath"
)

// Dir is $XDG_CACHE_HOME/agents when XDG_CACHE_HOME is set, else
// os.UserCacheDir()/agents. If the legacy chezmoi-agents directory exists
// and agents does not, it falls back to the existing directory.
func Dir(getenv func(string) string) (string, error) {
	base := "agents"
	legacy := "chezmoi-agents"
	if x := getenv("XDG_CACHE_HOME"); x != "" {
		p := filepath.Join(x, base)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		leg := filepath.Join(x, legacy)
		if _, err := os.Stat(leg); err == nil {
			return leg, nil
		}
		return p, nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("user cache dir: %w", err)
	}
	p := filepath.Join(d, base)
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	leg := filepath.Join(d, legacy)
	if _, err := os.Stat(leg); err == nil {
		return leg, nil
	}
	return p, nil
}
