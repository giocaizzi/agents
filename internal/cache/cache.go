// Package cache locates the agents cache directory for callers that have no
// resolved configuration; the commands read it from config.Dirs.Cache.
package cache

import (
	"fmt"
	"os"
	"path/filepath"
)

// Dir is $XDG_CACHE_HOME/agents when XDG_CACHE_HOME is set, else
// os.UserCacheDir()/agents.
func Dir(getenv func(string) string) (string, error) {
	if x := getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "agents"), nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("user cache dir: %w", err)
	}
	return filepath.Join(d, "agents"), nil
}
