package parity

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/giocaizzi/agents/internal/finding"
	"github.com/giocaizzi/agents/internal/policy"
)

const (
	vscodeSettingsRel  = "dot_config/Code/User/settings.json"
	claudeTemplateRel  = ".chezmoitemplates/claude-settings.json"
	copilotTemplateRel = ".chezmoitemplates/copilot-settings.json"
	packagesRel        = ".chezmoidata/packages.toml"
)

// CheckSourceFiles compares the chezmoi source directory with the policy: the
// static VS Code settings file must hold the native voice values, the Claude
// and Copilot templates must read them from the policy, and every LSP server must name a
// package row in packages.toml.
func CheckSourceFiles(p *policy.Policy, sourceDir string) []finding.Finding {
	fs := checkVSCodeFile(p, filepath.Join(sourceDir, filepath.FromSlash(vscodeSettingsRel)))
	fs = append(fs, checkClaudeTemplate(filepath.Join(sourceDir, filepath.FromSlash(claudeTemplateRel)))...)
	fs = append(fs, checkCopilotTemplate(filepath.Join(sourceDir, filepath.FromSlash(copilotTemplateRel)))...)
	return append(fs, checkLSPPackages(p, filepath.Join(sourceDir, filepath.FromSlash(packagesRel)))...)
}

func checkLSPPackages(p *policy.Policy, path string) []finding.Finding {
	const where = "lsp"
	if len(p.LSP.Server) == 0 {
		return nil
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return []finding.Finding{finding.New(where, "cannot read %s: %v", path, err)}
	}
	var reg struct {
		Package map[string]any `toml:"package"`
	}
	if err := toml.Unmarshal(data, &reg); err != nil {
		return []finding.Finding{finding.New(where, "cannot parse %s: %v", path, err)}
	}

	var fs []finding.Finding
	for _, name := range slices.Sorted(maps.Keys(p.LSP.Server)) {
		pkg := p.LSP.Server[name].Package
		if pkg == "" {
			continue // reported by policy validation
		}
		if _, ok := reg.Package[pkg]; !ok {
			fs = append(fs, finding.New(where, "lsp.server.%s.package %q is not in packages.toml", name, pkg))
		}
	}
	return fs
}

func checkVSCodeFile(p *policy.Policy, path string) []finding.Finding {
	const where = "vscode"
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return []finding.Finding{finding.New(where, "cannot read %s: %v", path, err)}
	}
	var settings map[string]any
	if err := json.Unmarshal(stripJSONC(data), &settings); err != nil {
		return []finding.Finding{finding.New(where, "cannot parse %s: %v", path, err)}
	}

	var fs []finding.Finding
	dictation := true // VS Code default when the key is absent
	if v, ok := settings["dictation.enabled"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			fs = append(fs, finding.New(where, "%s: dictation.enabled must be a boolean; edit settings.json to match the policy", path))
		}
		dictation = b
	}
	if dictation != p.Voice.Enabled {
		fs = append(fs, finding.New(where,
			"%s: dictation.enabled=%v but policy voice.enabled=%v; edit settings.json to match the policy",
			path, dictation, p.Voice.Enabled))
	}

	lang, hasLang := settings["agents.voice.language"]
	if p.Voice.Language == "" {
		if hasLang && lang != "auto" {
			fs = append(fs, finding.New(where,
				"%s: agents.voice.language=%v but policy voice.language is empty; edit settings.json to match the policy (remove the key)",
				path, lang))
		}
	} else if lang != p.Voice.Language {
		fs = append(fs, finding.New(where,
			"%s: agents.voice.language=%v but policy voice.language=%q; edit settings.json to match the policy",
			path, lang, p.Voice.Language))
	}
	return fs
}

func checkClaudeTemplate(path string) []finding.Finding {
	const where = "claude"
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return []finding.Finding{finding.New(where, "cannot read %s: %v", path, err)}
	}
	text := string(data)

	var fs []finding.Finding
	for _, field := range []string{"enabled", "mode", "autoSubmit"} {
		refAgents := ".agents.voice." + field
		refPolicy := ".harnessPolicy.voice." + field
		if !strings.Contains(text, refAgents) && !strings.Contains(text, refPolicy) {
			fs = append(fs, finding.New(where, "%s must read %s (or %s) from the configuration", path, refAgents, refPolicy))
		}
	}
	if strings.Contains(text, `"language":`) {
		fs = append(fs, finding.New(where,
			"%s sets a \"language\" key; it also sets Claude's reply language, so it is deliberately unmanaged", path))
	}
	return fs
}

func checkCopilotTemplate(path string) []finding.Finding {
	const where = "copilot"
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return []finding.Finding{finding.New(where, "cannot read %s: %v", path, err)}
	}
	text := string(data)
	const refAgents = ".agents.voice.enabled"
	const refPolicy = ".harnessPolicy.voice.enabled"
	if !strings.Contains(text, refAgents) && !strings.Contains(text, refPolicy) {
		return []finding.Finding{finding.New(where, "%s must read %s (or %s) from the configuration", path, refAgents, refPolicy)}
	}
	return nil
}

// stripJSONC turns JSONC into JSON: it drops // and /* */ comments outside
// strings and trailing commas before } or ].
func stripJSONC(src []byte) []byte {
	out := make([]byte, 0, len(src))
	for i := 0; i < len(src); {
		switch {
		case src[i] == '"':
			end := stringEnd(src, i)
			out = append(out, src[i:end]...)
			i = end
		case bytes.HasPrefix(src[i:], []byte("//")):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case bytes.HasPrefix(src[i:], []byte("/*")):
			end := bytes.Index(src[i+2:], []byte("*/"))
			if end < 0 {
				return out
			}
			i += end + 4
		default:
			out = append(out, src[i])
			i++
		}
	}
	return dropTrailingCommas(out)
}

// stringEnd returns the index just past the JSON string that starts at src[start].
func stringEnd(src []byte, start int) int {
	for i := start + 1; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(src)
}

func dropTrailingCommas(src []byte) []byte {
	out := make([]byte, 0, len(src))
	for i := 0; i < len(src); {
		switch {
		case src[i] == '"':
			end := stringEnd(src, i)
			out = append(out, src[i:end]...)
			i = end
		case src[i] == ',' && closesNext(src[i+1:]):
			i++
		default:
			out = append(out, src[i])
			i++
		}
	}
	return out
}

// closesNext reports whether the next non-space byte closes an object or array.
func closesNext(rest []byte) bool {
	rest = bytes.TrimLeft(rest, " \t\r\n")
	return len(rest) > 0 && (rest[0] == '}' || rest[0] == ']')
}
