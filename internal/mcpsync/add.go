package mcpsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AddOptions holds the parameters for adding or updating an MCP server.
type AddOptions struct {
	Name    string
	Type    string
	URL     string
	Headers map[string]string
	Command string
	Args    []string
	Env     map[string]string
}

// AddServer adds or updates an MCP server entry in <root>/.mcp.json.
func AddServer(root string, opts AddOptions) error {
	if !serverNameRe.MatchString(opts.Name) {
		return fmt.Errorf("invalid server name %q: must match ^[a-zA-Z0-9_-]+$", opts.Name)
	}

	normOpts, normErr := normalizeAndValidateAddOptions(opts)
	if normErr != nil {
		return normErr
	}

	srcPath := filepath.Clean(filepath.Join(root, SourceFilename))
	if chkErr := checkSourcePath(root, srcPath); chkErr != nil {
		return chkErr
	}

	src, readErr := readExistingSource(srcPath)
	if readErr != nil {
		return readErr
	}

	if colErr := checkCollisionWithOthers(normOpts.Name, src.MCPServers); colErr != nil {
		return colErr
	}

	src.MCPServers[normOpts.Name] = ServerEntry{
		Type:    normOpts.Type,
		URL:     normOpts.URL,
		Headers: normOpts.Headers,
		Command: normOpts.Command,
		Args:    normOpts.Args,
		Env:     normOpts.Env,
	}

	return writeSourceFile(srcPath, src)
}

func normalizeAndValidateAddOptions(opts AddOptions) (AddOptions, error) {
	if opts.Type == "" {
		if err := inferAddTransport(&opts); err != nil {
			return opts, err
		}
	}

	switch opts.Type {
	case TransportHTTP:
		return opts, validateAddHTTPTransport(opts)
	case TransportStdio:
		return opts, validateAddStdioTransport(opts)
	default:
		return opts, fmt.Errorf("unsupported transport type %q (supported: %s, %s)", opts.Type, TransportHTTP, TransportStdio)
	}
}

func inferAddTransport(opts *AddOptions) error {
	switch {
	case opts.URL != "" || strings.HasPrefix(opts.Command, "http://") || strings.HasPrefix(opts.Command, "https://"):
		opts.Type = TransportHTTP
		if opts.URL == "" {
			opts.URL = opts.Command
			opts.Command = ""
		}
		return nil
	case opts.Command != "":
		opts.Type = TransportStdio
		return nil
	default:
		return errors.New("must specify either command or url")
	}
}

func validateAddHTTPTransport(opts AddOptions) error {
	if opts.URL == "" {
		return errors.New("http transport requires a URL")
	}
	if opts.Command != "" || len(opts.Args) > 0 || len(opts.Env) > 0 {
		return errors.New("command, args and env not allowed for http transport")
	}
	resolved, err := resolvePlaceholders(opts.URL, "example-account")
	if err != nil {
		return fmt.Errorf("invalid url placeholder: %w", err)
	}
	if err := validateURL(resolved); err != nil {
		return fmt.Errorf("invalid url %q: %w", opts.URL, err)
	}
	return nil
}

func validateAddStdioTransport(opts AddOptions) error {
	if opts.Command == "" {
		return errors.New("stdio transport requires a command")
	}
	if opts.URL != "" || len(opts.Headers) > 0 {
		return errors.New("url and headers not allowed for stdio transport")
	}
	return nil
}

func readExistingSource(srcPath string) (Source, error) {
	src := Source{
		MCPServers: make(map[string]ServerEntry),
	}

	cleanPath := filepath.Clean(srcPath)
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return src, nil
		}
		return src, fmt.Errorf("read %s: %w", SourceFilename, err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if decErr := dec.Decode(&src); decErr != nil {
		return src, fmt.Errorf("invalid existing %s: %w", SourceFilename, decErr)
	}
	if src.MCPServers == nil {
		src.MCPServers = make(map[string]ServerEntry)
	}
	return src, nil
}

func checkCollisionWithOthers(name string, servers map[string]ServerEntry) error {
	normName := strings.ReplaceAll(name, "-", "_")
	for existingName := range servers {
		if existingName == name {
			continue
		}
		existingNorm := strings.ReplaceAll(existingName, "-", "_")
		if existingNorm == normName {
			return fmt.Errorf("namespace collision: %q and %q both normalize to %q", existingName, name, normName)
		}
	}
	return nil
}

func writeSourceFile(srcPath string, src Source) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(src); err != nil {
		return fmt.Errorf("encode %s: %w", SourceFilename, err)
	}

	cleanPath := filepath.Clean(srcPath)
	if err := os.WriteFile(cleanPath, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", cleanPath, err)
	}
	return nil
}

func checkSourcePath(root, srcPath string) error {
	rel, err := filepath.Rel(root, srcPath)
	if err != nil {
		return fmt.Errorf("rel: %w", err)
	}

	parts := strings.Split(rel, string(filepath.Separator))
	curr := root
	for _, part := range parts {
		curr = filepath.Join(curr, part)
		fi, statErr := os.Lstat(curr)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				break
			}
			return fmt.Errorf("lstat %s: %w", curr, statErr)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks not allowed in source or parent path: %s", curr)
		}
	}

	cleanSrc := filepath.Clean(srcPath)
	fi, statErr := os.Lstat(cleanSrc)
	if statErr == nil && fi.IsDir() {
		return fmt.Errorf("source %s is a directory", srcPath)
	}
	return nil
}
