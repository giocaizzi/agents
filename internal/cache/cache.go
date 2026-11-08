// Package cache locates the chezmoi-agents cache directory, shared by every
// subcommand that caches data (usage.json, ...).
package cache

import (
	"fmt"
	"os"
	"path/filepath"
)

// Dir is $XDG_CACHE_HOME/chezmoi-agents when XDG_CACHE_HOME is set, else
// os.UserCacheDir()/chezmoi-agents. It does not create the directory.
func Dir(getenv func(string) string) (string, error) {
	if x := getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "chezmoi-agents"), nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("user cache dir: %w", err)
	}
	return filepath.Join(d, "chezmoi-agents"), nil
}
