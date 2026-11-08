package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (d Deps) runPrompt(_ context.Context, args []string) int {
	fs := newFlags("prompt")
	profileFlag := fs.String("profile", "default", "target profile name (e.g. default, personal, work, light)")
	harnessFlag := fs.String("harness", "", "target client harness (claude, antigravity, copilot, codex, pi, opencode)")
	checkOnly := fs.Bool("check", false, "only check that prompt layers exist and assemble cleanly")
	fileFlag := fs.String("file", "", "optional path to policy or layer file")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(d.Stderr, "flag parse error: %v\n", err)
		return 2
	}

	profile := strings.ToLower(strings.TrimSpace(*profileFlag))
	if profile == "" {
		profile = "default"
	}

	if *harnessFlag != "" {
		return d.runHarnessPrompt(strings.ToLower(strings.TrimSpace(*harnessFlag)), profile, *fileFlag, *checkOnly)
	}

	if code, handled := d.tryLayerFile(*fileFlag, *checkOnly); handled {
		return code
	}

	if out, ok, err := d.tryPolicyProfile(*fileFlag, profile, *checkOnly); ok {
		if err != nil {
			fmt.Fprintf(d.Stderr, "%v\n", err)
			return 1
		}
		if out != "" {
			fmt.Fprint(d.Stdout, out)
		}
		return 0
	}

	if profile == "light" {
		if *checkOnly {
			fmt.Fprintln(d.Stdout, "✓ prompt (light: no instructions rendered)")
		}
		return 0
	}

	if out, ok, err := d.tryTemplatePrompt(profile, *checkOnly); ok {
		if err != nil {
			fmt.Fprintf(d.Stderr, "%v\n", err)
			return 1
		}
		fmt.Fprint(d.Stdout, out)
		return 0
	}

	fmt.Fprintf(d.Stderr, "unknown profile %q (no instructions found in configuration or prompt layers)\n", profile)
	return 1
}

func (d Deps) runHarnessPrompt(harness, profile, file string, checkOnly bool) int {
	content, origin, err := d.resolveHarnessPrompt(harness, profile, file)
	if err != nil {
		fmt.Fprintf(d.Stderr, "%v\n", err)
		return 1
	}
	if checkOnly {
		fmt.Fprintf(d.Stdout, "✓ prompt (%s: %s, %d bytes)\n", harness, origin, len(content))
		return 0
	}
	fmt.Fprint(d.Stdout, content)
	return 0
}

func (d Deps) tryLayerFile(file string, checkOnly bool) (int, bool) {
	if file == "" || (!strings.HasSuffix(file, ".md") && !strings.HasSuffix(file, ".age")) {
		return 0, false
	}
	data, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		fmt.Fprintf(d.Stderr, "read layer %s: %v\n", file, err)
		return 1, true
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		fmt.Fprintf(d.Stderr, "layer %s is empty\n", file)
		return 1, true
	}
	if checkOnly {
		fmt.Fprintf(d.Stdout, "✓ prompt layer %s (%d bytes)\n", filepath.Base(file), len(data))
		return 0, true
	}
	fmt.Fprint(d.Stdout, string(data))
	return 0, true
}

func (d Deps) tryPolicyProfile(file, profile string, checkOnly bool) (string, bool, error) {
	p, polPath, err := d.loadPolicy(file)
	if err != nil {
		return "", false, err
	}
	if p == nil || len(p.Profiles) == 0 {
		return "", false, nil
	}
	prof, ok := p.Profiles[profile]
	if !ok {
		return "", false, nil
	}
	if len(prof.Instructions) == 0 {
		if checkOnly {
			return fmt.Sprintf("✓ prompt (%s: no instructions configured)\n", profile), true, nil
		}
		return "", true, nil
	}

	content, err := assembleLayers(filepath.Dir(polPath), prof.Instructions)
	if err != nil {
		return "", true, err
	}
	if checkOnly {
		return fmt.Sprintf("✓ prompt (%s profile: %d layer(s) present)\n", profile, len(prof.Instructions)), true, nil
	}
	return content, true, nil
}

func assembleLayers(baseDir string, instructions []string) (string, error) {
	var sb strings.Builder
	for _, layerRel := range instructions {
		layerPath := layerRel
		if !filepath.IsAbs(layerPath) {
			layerPath = filepath.Join(baseDir, layerRel)
		}
		data, err := os.ReadFile(filepath.Clean(layerPath))
		if err != nil {
			return "", fmt.Errorf("missing prompt layer %s: %w", layerPath, err)
		}
		sb.Write(data)
		if !strings.HasSuffix(sb.String(), "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String(), nil
}

func (d Deps) tryTemplatePrompt(profile string, checkOnly bool) (string, bool, error) {
	corePath := d.findCorePath()
	if corePath == "" {
		return "", false, nil
	}

	coreBytes, err := os.ReadFile(filepath.Clean(corePath))
	if err != nil {
		return "", true, fmt.Errorf("missing core prompt layer %s: %w", corePath, err)
	}

	dir := filepath.Dir(corePath)
	envBytes := readOptional(filepath.Join(dir, "env.md"))
	hasPriv := checkPrivLayer(dir, profile)

	if checkOnly {
		layerDesc := "core"
		if hasPriv {
			layerDesc += " + " + profile
		}
		return fmt.Sprintf("✓ prompt (%s profile: %s layer present)\n", profile, layerDesc), true, nil
	}

	var sb strings.Builder
	sb.Write(coreBytes)
	ensureNewline(&sb)
	if len(envBytes) > 0 {
		sb.Write(envBytes)
		ensureNewline(&sb)
	}
	if hasPriv {
		fmt.Fprintf(&sb, "\n<!-- Profile Layer: %s -->\n", profile)
	}
	return sb.String(), true, nil
}

func readOptional(path string) []byte {
	if _, err := os.Stat(filepath.Clean(path)); err == nil {
		data, _ := os.ReadFile(filepath.Clean(path))
		return data
	}
	return nil
}

func checkPrivLayer(dir, profile string) bool {
	privPath := filepath.Join(dir, profile+".md.age")
	if _, err := os.Stat(filepath.Clean(privPath)); err == nil {
		return true
	}
	privFallback := filepath.Join(dir, profile+".md")
	_, err := os.Stat(filepath.Clean(privFallback))
	return err == nil
}

func ensureNewline(sb *strings.Builder) {
	if !strings.HasSuffix(sb.String(), "\n") {
		sb.WriteString("\n")
	}
}

func (d Deps) findCorePath() string {
	srcDir := d.findSourceDir()
	candidates := []string{
		filepath.Join(srcDir, ".chezmoitemplates", "agents", "core.md"),
		filepath.Join(srcDir, "prompts", "core.md"),
		filepath.Join("prompts", "core.md"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Clean(c)); err == nil {
			return c
		}
	}
	return ""
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
