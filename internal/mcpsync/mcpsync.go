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
	"regexp"
	"slices"
	"strings"
)

// Names of the source file and of the native files derived from it.
const (
	SourceFilename     = ".mcp.json"
	CodexTargetRelPath = ".codex/config.toml"
	PiTargetRelPath    = ".pi/mcp.json"
	AntigravityRelPath = ".agents/mcp_config.json"

	// AntigravityRedirectURI is the fixed OAuth redirect URI of Antigravity.
	AntigravityRedirectURI = "https://antigravity.google/oauth-callback"

	TransportHTTP  = "http"
	TransportStdio = "stdio"
)

var (
	serverNameRe   = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	accountParamRe = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
	placeholderRe  = regexp.MustCompile(`\$\{([^}]+)\}`)
)

// ServerEntry matches the schema for an MCP server definition.
//
// A pre-registered OAuth client may be declared in either key style that
// shares the source file: Claude Code's nested OAuth object, or GitHub Copilot
// CLI's flat OAuthClientID, OAuthPublicClient and Auth keys. Both are kept
// verbatim so that rewriting the source never drops either style. A client
// secret is never part of the schema.
type ServerEntry struct {
	Type              string            `json:"type,omitempty"`
	URL               string            `json:"url,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	Command           string            `json:"command,omitempty"`
	Args              []string          `json:"args,omitempty"`
	Env               map[string]string `json:"env,omitempty"`
	OAuth             *OAuth            `json:"oauth,omitempty"`
	OAuthClientID     string            `json:"oauthClientId,omitempty"`
	OAuthPublicClient *bool             `json:"oauthPublicClient,omitempty"`
	Auth              *Auth             `json:"auth,omitempty"`
}

// OAuth is Claude Code's nested pre-registered OAuth client.
type OAuth struct {
	ClientID     string `json:"clientId,omitempty"`
	CallbackPort int    `json:"callbackPort,omitempty"`
}

// oauthClient returns the effective pre-registered OAuth client id and
// callback port. Per field, the nested (Claude Code) style wins over the flat
// (Copilot CLI) one; differing values are allowed because the harnesses may
// legitimately use different ports. OAuthPublicClient is Copilot-only and is
// never translated.
func (e ServerEntry) oauthClient() (clientID string, port int) {
	clientID = e.OAuthClientID
	if e.OAuth != nil && e.OAuth.ClientID != "" {
		clientID = e.OAuth.ClientID
	}
	if e.Auth != nil {
		port = e.Auth.RedirectPort
	}
	if e.OAuth != nil && e.OAuth.CallbackPort != 0 {
		port = e.OAuth.CallbackPort
	}
	return clientID, port
}

// Auth is GitHub Copilot CLI's OAuth redirect settings.
type Auth struct {
	RedirectPort int `json:"redirectPort,omitempty"`
}

// Source represents the top-level .mcp.json schema.
type Source struct {
	MCPServers map[string]ServerEntry `json:"mcpServers"`
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
	if entry.OAuth != nil && !validPort(entry.OAuth.CallbackPort) {
		return entry, fmt.Errorf("server %q: oauth.callbackPort %d out of range 1..65535", name, entry.OAuth.CallbackPort)
	}
	if entry.Auth != nil && !validPort(entry.Auth.RedirectPort) {
		return entry, fmt.Errorf("server %q: auth.redirectPort %d out of range 1..65535", name, entry.Auth.RedirectPort)
	}

	if id, _ := entry.oauthClient(); !validClientID(id) || !validClientID(entry.OAuthClientID) {
		return entry, fmt.Errorf("server %q: oauth client id must not contain control characters, quotes or backslashes", name)
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
	if entry.OAuth != nil || entry.OAuthClientID != "" || entry.OAuthPublicClient != nil || entry.Auth != nil {
		return entry, fmt.Errorf("server %q: oauth, oauthClientId, oauthPublicClient and auth not allowed for stdio transport", name)
	}
	return entry, nil
}

// validClientID reports whether an OAuth client id is free of the characters
// that need escaping in the generated files: control characters, quotes and
// backslashes.
func validClientID(id string) bool {
	return !strings.ContainsFunc(id, func(r rune) bool {
		return r < 0x20 || r == 0x7f || r == '"' || r == '\\'
	})
}

// validPort reports whether p is unset (0) or a valid TCP port.
func validPort(p int) bool {
	return p == 0 || (p >= 1 && p <= 65535)
}

func resolvePlaceholders(rawURL, account string) (string, error) {
	matches := placeholderRe.FindAllStringSubmatch(rawURL, -1)
	resolved := rawURL
	for _, match := range matches {
		fullMatch := match[0]
		varName := match[1]
		if varName == "MCP_CONNECTOR_ACCOUNT" {
			if account == "" || !accountParamRe.MatchString(account) {
				return "", fmt.Errorf("account parameter is required and must match ^[a-zA-Z0-9-]+$, got %q", account)
			}
			resolved = strings.ReplaceAll(resolved, fullMatch, account)
		} else {
			envVal := os.Getenv(varName)
			if envVal == "" {
				return "", fmt.Errorf("environment variable %q required for placeholder %q is not set", varName, fullMatch)
			}
			resolved = strings.ReplaceAll(resolved, fullMatch, envVal)
		}
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

// Warnings lists the source settings that a target cannot represent and that
// a sync therefore leaves out of it (a callback port for Antigravity), and the confidential OAuth clients
// (oauthPublicClient false or unset) whose secret agents never syncs: it
// lives in the harness's keyring.
func Warnings(servers map[string]ServerEntry) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	slices.Sort(names)

	var warnings []string
	for _, name := range names {
		entry := servers[name]
		if entry.Type != TransportHTTP {
			continue
		}
		clientID, port := entry.oauthClient()
		if port != 0 {
			warnings = append(warnings, fmt.Sprintf("antigravity: server %q: callback port %d not supported, left out (register %s as a redirect URI)", name, port, AntigravityRedirectURI))
		}
		if clientID != "" && (entry.OAuthPublicClient == nil || !*entry.OAuthPublicClient) {
			warnings = append(warnings, fmt.Sprintf("server %q: OAuth client is not public, so its secret is not synced; add the secret through the harness's own login", name))
		}
	}
	return warnings
}
