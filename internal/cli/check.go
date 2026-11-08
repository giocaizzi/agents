package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/finding"
	"github.com/giocaizzi/agents/internal/parity"
	"github.com/giocaizzi/agents/internal/pluginreg"
)

func (d Deps) runCheck(ctx context.Context, args []string) int {
	fs := newFlags("check")
	fileFlag := fs.String("file", "", "path to harness-policy.toml or harness.toml")
	harnessDirFlag := fs.String("harness-dir", d.Getenv("HARNESS_DIR"), "a checkout of giocaizzi/harness")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(d.Stderr, "flag parse error: %v\n", err)
		return 2
	}

	findings, msgs, err := d.checkTarget(ctx, *fileFlag, *harnessDirFlag)
	if err != nil {
		fmt.Fprintf(d.Stderr, "check error: %v\n", err)
		return 1
	}
	if len(findings) > 0 {
		fmt.Fprintln(d.Stderr, formatFindings(findings))
		return 1
	}
	for _, m := range msgs {
		fmt.Fprintln(d.Stdout, m)
	}
	return 0
}

func (d Deps) checkTarget(ctx context.Context, file, harnessDir string) ([]finding.Finding, []string, error) {
	if file != "" {
		if isHarnessToml(file) {
			findings, err := pluginreg.ValidateFile(ctx, file, harnessDir)
			if err != nil {
				return nil, nil, fmt.Errorf("validate plugins: %w", err)
			}
			return findings, []string{fmt.Sprintf("✓ harness plugins (%s)", file)}, nil
		}

		p, path, err := d.loadPolicy(file)
		if err != nil {
			return nil, nil, err
		}
		findings := p.Validate(path)
		findings = append(findings, parity.CheckParity(p)...)
		msgs := []string{fmt.Sprintf("✓ policy & harness parity (%s)", path)}
		findings, msgs = d.appendSourceFiles(p, findings, msgs)
		return findings, msgs, nil
	}

	return d.checkAll(ctx, harnessDir)
}

func (d Deps) checkAll(ctx context.Context, harnessDir string) ([]finding.Finding, []string, error) {
	var allFindings []finding.Finding
	var msgs []string

	p, polPath, err := d.loadPolicy("")
	if err != nil {
		return nil, nil, fmt.Errorf("load policy: %w", err)
	}
	findings := p.Validate(polPath)
	findings = append(findings, parity.CheckParity(p)...)
	msgs = append(msgs, fmt.Sprintf("✓ policy & harness parity (%s)", polPath))
	findings, msgs = d.appendSourceFiles(p, findings, msgs)
	allFindings = append(allFindings, findings...)

	harnessTomlPath, err := d.findHarnessToml("")
	if err == nil {
		pFindings, pErr := pluginreg.ValidateFile(ctx, harnessTomlPath, harnessDir)
		if pErr != nil {
			return nil, nil, fmt.Errorf("validate plugins: %w", pErr)
		}
		allFindings = append(allFindings, pFindings...)
		msgs = append(msgs, fmt.Sprintf("✓ harness plugins (%s)", harnessTomlPath))
	}

	return allFindings, msgs, nil
}

// appendSourceFiles checks the native voice settings in the discovered chezmoi
// source directory; a missing source directory is a finding, not a skip.
func (d Deps) appendSourceFiles(p *config.Policy, findings []finding.Finding, msgs []string) ([]finding.Finding, []string) {
	srcDir, err := d.findPolicySourceDir()
	if err != nil {
		return append(findings, finding.New("source", "%v", err)), msgs
	}
	findings = append(findings, parity.CheckSourceFiles(p, srcDir)...)
	return findings, append(msgs, fmt.Sprintf("✓ native voice settings (%s)", srcDir))
}

func isHarnessToml(path string) bool {
	if strings.HasSuffix(path, "harness.toml") {
		return true
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "harnessPlugins")
}
