package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (d Deps) runPrompt(_ context.Context, args []string) int {
	fs := newFlags("prompt")
	profileFlag := fs.String("profile", "personal", "target profile (personal, work, light)")
	checkOnly := fs.Bool("check", false, "only check that prompt layers exist and assemble cleanly")
	fileFlag := fs.String("file", "", "optional path to edited prompt layer file")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(d.Stderr, "flag parse error: %v\n", err)
		return 2
	}

	if *fileFlag != "" {
		cleaned := filepath.Clean(*fileFlag)
		data, err := os.ReadFile(cleaned)
		if err != nil {
			fmt.Fprintf(d.Stderr, "error reading layer file %s: %v\n", *fileFlag, err)
			return 1
		}
		if len(bytes.TrimSpace(data)) == 0 {
			fmt.Fprintf(d.Stderr, "layer file %s is empty\n", *fileFlag)
			return 1
		}
	}

	profile := strings.ToLower(*profileFlag)
	if profile != "personal" && profile != "work" && profile != "light" {
		fmt.Fprintf(d.Stderr, "invalid profile %q (expected personal, work, or light)\n", profile)
		return 2
	}

	if profile == "light" {
		if *checkOnly {
			fmt.Fprintln(d.Stdout, "✓ prompt (light: no instructions rendered)")
		}
		return 0
	}

	srcDir := d.findSourceDir()
	corePath := filepath.Join(srcDir, ".chezmoitemplates", "agents", "core.md")
	envPath := filepath.Join(srcDir, ".chezmoitemplates", "agents", "env.md")
	privPath := filepath.Join(srcDir, ".chezmoitemplates", "agents", profile+".md.age")

	coreBytes, err := os.ReadFile(filepath.Clean(corePath))
	if err != nil {
		fmt.Fprintf(d.Stderr, "missing core prompt layer %s: %v\n", corePath, err)
		return 1
	}

	envBytes, err := os.ReadFile(filepath.Clean(envPath))
	if err != nil {
		fmt.Fprintf(d.Stderr, "missing env prompt layer %s: %v\n", envPath, err)
		return 1
	}

	if _, err := os.Stat(filepath.Clean(privPath)); err != nil {
		// Try unencrypted fallback
		privFallback := filepath.Join(srcDir, ".chezmoitemplates", "agents", profile+".md")
		if _, err := os.Stat(filepath.Clean(privFallback)); err != nil {
			fmt.Fprintf(d.Stderr, "missing profile prompt layer %s: %v\n", privPath, err)
			return 1
		}
	}

	if *checkOnly {
		fmt.Fprintf(d.Stdout, "✓ prompt (%s profile: core + env + %s layer present)\n", profile, profile)
		return 0
	}

	var sb strings.Builder
	sb.Write(coreBytes)
	if !strings.HasSuffix(sb.String(), "\n") {
		sb.WriteString("\n")
	}
	sb.Write(envBytes)
	if !strings.HasSuffix(sb.String(), "\n") {
		sb.WriteString("\n")
	}
	fmt.Fprintf(&sb, "\n<!-- Profile Layer: %s (age-encrypted in repository) -->\n", profile)

	fmt.Fprint(d.Stdout, sb.String())
	return 0
}

func (d Deps) findSourceDir() string {
	if s := d.Getenv("CHEZMOI_SOURCE_DIR"); s != "" {
		return s
	}
	candidates := []string{
		"home",
		".",
		filepath.Join("..", "home"),
		filepath.Join("..", "..", "home"),
		filepath.Join("..", "..", "..", "home"),
		filepath.Join("..", "..", "..", "..", "home"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(filepath.Join(c, ".chezmoitemplates", "agents", "core.md")); err == nil && !fi.IsDir() {
			return c
		}
	}
	return "home"
}
