// Package arch_test holds the architecture fitness checks: rules about the
// repository itself that fail `go test ./...` instead of living in a document.
package arch_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/giocaizzi/agents"

// moduleRoot is the directory holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	return filepath.Dir(strings.TrimSpace(string(out)))
}

// trackedFiles lists the git-tracked files of the module root.
func trackedFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "ls-files", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var files []string
	for f := range strings.SplitSeq(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

// grepTracked returns "file:line: text" for each line of a tracked text file
// that matches re. Binary files (a NUL byte) and unreadable ones are skipped.
func grepTracked(t *testing.T, re *regexp.Regexp, only func(path string) bool) []string {
	t.Helper()
	root := moduleRoot(t)
	var hits []string
	for _, rel := range trackedFiles(t, root) {
		if only != nil && !only(rel) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				hits = append(hits, rel+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
	}
	return hits
}

// The tool is standalone: no file may name a dotfiles manager. The name is
// built from two halves so that this file does not match itself.
var managerName = regexp.MustCompile(`(?i)chez` + `moi`)

func TestNoDotfilesManagerReferences(t *testing.T) {
	t.Parallel()
	for _, hit := range grepTracked(t, managerName, nil) {
		t.Errorf("agents is standalone and knows no dotfiles manager; reword or delete: %s", hit)
	}
}

func TestNoManagerEnvVars(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`CHEZ` + `MOI_`)
	hits := grepTracked(t, re, func(p string) bool { return strings.HasSuffix(p, ".go") })
	for _, hit := range hits {
		t.Errorf("no Go file may read a manager environment variable; take the path from the config or a flag: %s", hit)
	}
}

// inward lists the packages that must not depend on the CLI that drives them.
var inward = []string{"harness", "config", "apply", "instructions"}

func TestInnerPackagesDoNotImportCLI(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	cli := modulePath + "/internal/cli"
	for _, name := range inward {
		pkg := modulePath + "/internal/" + name
		cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "-json=ImportPath", pkg)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list -deps %s: %v", pkg, err)
		}
		dec := json.NewDecoder(bytes.NewReader(out))
		for dec.More() {
			var p struct {
				ImportPath string `json:"ImportPath"`
			}
			if err := dec.Decode(&p); err != nil {
				t.Fatalf("decode go list output: %v", err)
			}
			if p.ImportPath == cli {
				t.Errorf("internal/%s depends on internal/cli; the CLI calls inward, so move the shared code into internal/%s or a new package both import", name, name)
			}
		}
	}
}
