// Package mcpsync parses, validates, and renders cross-harness MCP server
// configurations from a canonical root .mcp.json to downstream harness formats.
package mcpsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// CurrentMarker and LegacyMarker identify generated MCP configurations.
const (
	CurrentMarker = "chezmoi-agents mcp sync"
	LegacyMarker  = "harness/scripts/sync-mcp.mjs"

	SourceFilename     = ".mcp.json"
	CodexTargetRelPath = ".codex/config.toml"
	PiTargetRelPath    = ".pi/mcp.json"
	AntigravityRelPath = ".agents/mcp_config.json"

	TransportHTTP  = "http"
	TransportStdio = "stdio"

	placeholderAccount = "${MCP_CONNECTOR_ACCOUNT}"
)

var (
	serverNameRe   = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	accountParamRe = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
	placeholderRe  = regexp.MustCompile(`\$\{([^}]+)\}`)
)

// ServerEntry matches the schema for an MCP server definition.
type ServerEntry struct {
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Source represents the top-level .mcp.json schema.
type Source struct {
	MCPServers map[string]ServerEntry `json:"mcpServers"`
}

// OutputFile describes a generated target configuration file.
type OutputFile struct {
	Path    string
	Content []byte
}

// ParseAndValidate parses .mcp.json content and resolves placeholders.
func ParseAndValidate(r io.Reader, account string) (*Source, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read source: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var src Source
	if err := dec.Decode(&src); err != nil {
		return nil, fmt.Errorf("invalid .mcp.json schema: %w", err)
	}

	if dec.More() {
		return nil, errors.New("invalid JSON: unexpected trailing data")
	}

	if src.MCPServers == nil {
		src.MCPServers = make(map[string]ServerEntry)
	}

	if err := checkCollisions(src.MCPServers); err != nil {
		return nil, err
	}

	for name, entry := range src.MCPServers {
		validated, err := validateServerEntry(name, entry, account)
		if err != nil {
			return nil, err
		}
		src.MCPServers[name] = validated
	}

	return &src, nil
}

func checkCollisions(servers map[string]ServerEntry) error {
	normalized := make(map[string]string, len(servers))
	for name := range servers {
		if !serverNameRe.MatchString(name) {
			return fmt.Errorf("invalid server name %q: must match ^[a-zA-Z0-9_-]+$", name)
		}
		norm := strings.ReplaceAll(name, "-", "_")
		if orig, exists := normalized[norm]; exists {
			return fmt.Errorf("namespace collision: %q and %q both normalize to %q", orig, name, norm)
		}
		normalized[norm] = name
	}
	return nil
}

func validateServerEntry(name string, entry ServerEntry, account string) (ServerEntry, error) {
	t := entry.Type
	if t == "" {
		switch {
		case entry.Command != "":
			t = TransportStdio
		case entry.URL != "":
			t = TransportHTTP
		default:
			return entry, fmt.Errorf("server %q: missing required command or url", name)
		}
		entry.Type = t
	}

	switch t {
	case TransportHTTP:
		return validateHTTPEntry(name, entry, account)
	case TransportStdio:
		return validateStdioEntry(name, entry)
	default:
		return entry, fmt.Errorf("server %q: unsupported transport type %q (only %q and %q are supported)", name, entry.Type, TransportHTTP, TransportStdio)
	}
}

func validateHTTPEntry(name string, entry ServerEntry, account string) (ServerEntry, error) {
	if entry.URL == "" {
		return entry, fmt.Errorf("server %q: http transport requires url", name)
	}
	if entry.Command != "" || len(entry.Args) > 0 || len(entry.Env) > 0 {
		return entry, fmt.Errorf("server %q: command, args and env not allowed for http transport", name)
	}

	resolvedURL, err := resolvePlaceholders(entry.URL, account)
	if err != nil {
		return entry, fmt.Errorf("server %q: %w", name, err)
	}

	if err := validateURL(resolvedURL); err != nil {
		return entry, fmt.Errorf("server %q invalid url %q: %w", name, resolvedURL, err)
	}

	entry.URL = resolvedURL
	return entry, nil
}

func validateStdioEntry(name string, entry ServerEntry) (ServerEntry, error) {
	if entry.Command == "" {
		return entry, fmt.Errorf("server %q: stdio transport requires command", name)
	}
	if entry.URL != "" || len(entry.Headers) > 0 {
		return entry, fmt.Errorf("server %q: url and headers not allowed for stdio transport", name)
	}
	return entry, nil
}

func resolvePlaceholders(rawURL, account string) (string, error) {
	matches := placeholderRe.FindAllStringSubmatch(rawURL, -1)
	resolved := rawURL
	for _, match := range matches {
		fullMatch := match[0]
		varName := match[1]
		if fullMatch != placeholderAccount {
			return "", fmt.Errorf("unsupported placeholder %q: only ${MCP_CONNECTOR_ACCOUNT} is allowed", fullMatch)
		}
		if account == "" || !accountParamRe.MatchString(account) {
			return "", fmt.Errorf("account parameter is required and must match ^[a-zA-Z0-9-]+$, got %q", account)
		}
		resolved = strings.ReplaceAll(resolved, placeholderAccount, account)
		_ = varName
	}
	return resolved, nil
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("host must not be empty")
	}
	if u.User != nil {
		return errors.New("userinfo not allowed in URL")
	}
	if u.RawQuery != "" {
		return errors.New("query parameters not allowed in URL")
	}
	if u.Fragment != "" {
		return errors.New("fragments not allowed in URL")
	}
	return nil
}

// Check verifies that .mcp.json is valid and that preflight checks pass for target files.
func Check(root, account string) error {
	srcPath := filepath.Clean(filepath.Join(root, SourceFilename))
	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", SourceFilename, err)
	}
	defer f.Close()

	src, err := ParseAndValidate(f, account)
	if err != nil {
		return err
	}

	targets := []string{
		filepath.Join(root, CodexTargetRelPath),
		filepath.Join(root, PiTargetRelPath),
		filepath.Join(root, AntigravityRelPath),
	}

	for _, target := range targets {
		if err := PreflightCheck(root, target); err != nil {
			return err
		}
	}

	_ = src
	return nil
}

// PreflightCheck inspects target and parent paths for symlinks and checks file ownership markers.
func PreflightCheck(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return fmt.Errorf("rel: %w", err)
	}

	// Walk every path component from root down to target to ensure no symlinks
	parts := strings.Split(rel, string(filepath.Separator))
	curr := root
	for _, part := range parts {
		curr = filepath.Join(curr, part)
		fi, statErr := os.Lstat(curr)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				// Remaining components do not exist yet; safe to proceed
				break
			}
			return fmt.Errorf("lstat %s: %w", curr, statErr)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks not allowed in target or parent path: %s", curr)
		}
	}

	// If target file exists, check ownership
	targetClean := filepath.Clean(target)
	fi, err := os.Lstat(targetClean)
	if err == nil {
		if fi.IsDir() {
			return fmt.Errorf("target %s is a directory", target)
		}
		data, readErr := os.ReadFile(targetClean)
		if readErr != nil {
			return fmt.Errorf("read target %s: %w", target, readErr)
		}
		if !isOwned(target, data) {
			return fmt.Errorf("unowned file exists at %s", target)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check target %s: %w", target, err)
	}

	return nil
}

func isOwned(path string, content []byte) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".toml":
		s := string(content)
		return strings.HasPrefix(s, "# Generated by "+CurrentMarker) ||
			strings.HasPrefix(s, "# Generated by "+LegacyMarker)
	case ".json":
		var parsed struct {
			GeneratedBy string `json:"_generatedBy"`
		}
		if err := json.Unmarshal(content, &parsed); err != nil {
			return false
		}
		return parsed.GeneratedBy == CurrentMarker || parsed.GeneratedBy == LegacyMarker
	default:
		return false
	}
}

// Sync validates .mcp.json, checks preflight conditions, and writes target files with mode 0o600.
func Sync(root, account string) ([]OutputFile, error) {
	srcPath := filepath.Clean(filepath.Join(root, SourceFilename))
	f, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", SourceFilename, err)
	}
	defer f.Close()

	src, err := ParseAndValidate(f, account)
	if err != nil {
		return nil, err
	}

	codexContent := RenderCodex(src.MCPServers)
	piContent, err := RenderPi(src.MCPServers)
	if err != nil {
		return nil, fmt.Errorf("render pi config: %w", err)
	}
	agContent := RenderAntigravity(piContent)

	outputs := []OutputFile{
		{Path: filepath.Join(root, CodexTargetRelPath), Content: codexContent},
		{Path: filepath.Join(root, PiTargetRelPath), Content: piContent},
		{Path: filepath.Join(root, AntigravityRelPath), Content: agContent},
	}

	for _, out := range outputs {
		if err := PreflightCheck(root, out.Path); err != nil {
			return nil, err
		}
	}

	for _, out := range outputs {
		dir := filepath.Dir(out.Path)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", dir, err)
		}
		if err := os.WriteFile(filepath.Clean(out.Path), out.Content, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", out.Path, err)
		}
	}

	return outputs, nil
}
