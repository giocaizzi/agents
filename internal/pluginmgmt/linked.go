package pluginmgmt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// maxStubSize caps the config files read to find links into the checkout.
const maxStubSize = 1 << 20

// statusLinked is the installed value of a live plugin without a version.
const statusLinked = "linked"

// checkoutVersion returns the version of plugin in the registry checkout, or
// "linked" when it has none.
func (m Manager) checkoutVersion(plugin string) string {
	if v := ReadPluginVersion(filepath.Join(m.HarnessDir, "plugins", plugin)); v != "" {
		return v
	}
	return statusLinked
}

// piVersion reports plugin as installed when pi loads the registry checkout as
// a package, which makes it live: pi runs whatever the checkout holds.
func (m Manager) piVersion(plugin string) string {
	if m.HarnessDir == "" {
		return ""
	}
	settings := filepath.Join(m.HomeDir, ".pi", "agent", "settings.json")
	data, err := os.ReadFile(filepath.Clean(settings))
	if err != nil {
		return ""
	}
	var cfg struct {
		Packages []json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	for _, raw := range cfg.Packages {
		src := packageSource(raw)
		if src == "" || isRemoteSource(src) {
			continue
		}
		switch {
		case src == "~" || strings.HasPrefix(src, "~/"):
			src = filepath.Join(m.HomeDir, strings.TrimPrefix(src, "~"))
		case !filepath.IsAbs(src):
			src = filepath.Join(filepath.Dir(settings), src)
		}
		if sameDir(src, m.HarnessDir) {
			return m.checkoutVersion(plugin)
		}
	}
	return ""
}

// packageSource extracts the source of a pi package entry: a string or an
// object with a "source" string.
func packageSource(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Source string `json:"source"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Source
	}
	return ""
}

func isRemoteSource(src string) bool {
	for _, prefix := range []string{"git:", "npm:", "http://", "https://", "ssh://"} {
		if strings.HasPrefix(src, prefix) {
			return true
		}
	}
	return false
}

// sameDir reports whether a and b are the same path, resolving symlinks when possible.
func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return a == b
}

func (m Manager) opencodeTarget() string {
	cfg := m.ConfigHome
	if cfg == "" {
		cfg = filepath.Join(m.HomeDir, ".config")
	}
	return filepath.Join(cfg, "opencode")
}

// opencodeVersion compares what the checkout's opencode installer copies and
// links into the opencode config with the checkout. It returns "" when the
// plugin is not installed or does not apply, VersionOutdated when an item is
// missing or a copy differs, and the checkout's version otherwise.
func (m Manager) opencodeVersion(plugin string) string {
	if m.HarnessDir == "" {
		return ""
	}
	src := filepath.Join(m.HarnessDir, "plugins", plugin)
	target := m.opencodeTarget()

	var total, present int
	differs := false

	for _, c := range opencodeCopies(src, target) {
		total++
		if _, err := os.Stat(c.dst); err != nil {
			continue
		}
		present++
		if !sameTree(c.src, c.dst) {
			differs = true
		}
	}
	links := []struct {
		hook  string
		files func() []string
	}{
		{"opencode.mjs", func() []string {
			files, _ := filepath.Glob(filepath.Join(target, "plugins", "*.js"))
			return files
		}},
		{"opencode-tui.mjs", func() []string { return []string{filepath.Join(target, "tui.json")} }},
	}
	for _, l := range links {
		if _, err := os.Stat(filepath.Join(src, "hooks", l.hook)); err != nil {
			continue
		}
		total++
		needle := "/plugins/" + plugin + "/hooks/" + l.hook
		if slices.ContainsFunc(l.files(), func(f string) bool { return fileContains(f, needle) }) {
			present++
		}
	}

	switch {
	case total == 0 || present == 0:
		return ""
	case present < total || differs:
		return VersionOutdated
	}
	return m.checkoutVersion(plugin)
}

type copyPair struct{ src, dst string }

// opencodeCopies lists the skill directories and generated agent files the
// installer copies for the plugin directory src.
func opencodeCopies(src, target string) []copyPair {
	var out []copyPair
	if entries, err := os.ReadDir(filepath.Join(src, "skills")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				out = append(out, copyPair{filepath.Join(src, "skills", e.Name()), filepath.Join(target, "skills", e.Name())})
			}
		}
	}
	if entries, err := os.ReadDir(filepath.Join(src, ".generated", "opencode")); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				out = append(out, copyPair{filepath.Join(src, ".generated", "opencode", e.Name()), filepath.Join(target, "agents", e.Name())})
			}
		}
	}
	return out
}

// fileContains reports whether the first maxStubSize bytes of file contain needle.
func fileContains(file, needle string) bool {
	f, err := os.Open(filepath.Clean(file))
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxStubSize))
	return err == nil && bytes.Contains(data, []byte(needle))
}

// sameTree reports whether a and b (files or directories) hold the same
// relative file set with the same bytes.
func sameTree(a, b string) bool {
	fa, err := treeFiles(a)
	if err != nil {
		return false
	}
	fb, err := treeFiles(b)
	if err != nil || !slices.Equal(fa, fb) {
		return false
	}
	for _, rel := range fa {
		da, errA := os.ReadFile(filepath.Clean(filepath.Join(a, rel)))
		db, errB := os.ReadFile(filepath.Clean(filepath.Join(b, rel)))
		if errA != nil || errB != nil || !bytes.Equal(da, db) {
			return false
		}
	}
	return true
}

// treeFiles lists the files under root as sorted relative paths; a plain file
// yields ".".
func treeFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return []string{"."}, nil
	}
	var files []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return fmt.Errorf("relative path of %s: %w", p, relErr)
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	slices.Sort(files)
	return files, nil
}
