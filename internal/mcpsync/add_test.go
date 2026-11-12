package mcpsync_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/mcpsync"
)

func TestAddServer_HTTP(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	err := mcpsync.AddServer(tmpDir, mcpsync.AddOptions{
		Name: "docs",
		Type: "http",
		URL:  "https://mcp.example.com/mcp",
		Headers: map[string]string{
			"Authorization": "Bearer token123",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error adding http server: %v", err)
	}

	srcFile := filepath.Join(tmpDir, mcpsync.SourceFilename)
	data, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}

	var src mcpsync.Source
	if err := json.Unmarshal(data, &src); err != nil {
		t.Fatalf("unmarshal .mcp.json: %v", err)
	}

	entry, ok := src.MCPServers["docs"]
	if !ok {
		t.Fatal("expected 'docs' server in .mcp.json")
	}
	if entry.Type != "http" || entry.URL != "https://mcp.example.com/mcp" {
		t.Errorf("unexpected entry values: %+v", entry)
	}
	if entry.Headers["Authorization"] != "Bearer token123" {
		t.Errorf("unexpected header: %v", entry.Headers)
	}
}

func TestAddServer_Stdio(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	err := mcpsync.AddServer(tmpDir, mcpsync.AddOptions{
		Name:    "context7",
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp"},
		Env: map[string]string{
			"API_KEY": "secret",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error adding stdio server: %v", err)
	}

	srcFile := filepath.Join(tmpDir, mcpsync.SourceFilename)
	data, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}

	var src mcpsync.Source
	if err := json.Unmarshal(data, &src); err != nil {
		t.Fatalf("unmarshal .mcp.json: %v", err)
	}

	entry, ok := src.MCPServers["context7"]
	if !ok {
		t.Fatal("expected 'context7' server in .mcp.json")
	}
	if entry.Type != "stdio" || entry.Command != "npx" {
		t.Errorf("unexpected entry values: %+v", entry)
	}
	if len(entry.Args) != 2 || entry.Args[1] != "@upstash/context7-mcp" {
		t.Errorf("unexpected args: %v", entry.Args)
	}
	if entry.Env["API_KEY"] != "secret" {
		t.Errorf("unexpected env: %v", entry.Env)
	}
}

func TestAddServer_AutoDetectType(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// 1. Auto detect http
	err := mcpsync.AddServer(tmpDir, mcpsync.AddOptions{
		Name:    "remote",
		Command: "https://api.example.com/mcp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2. Auto detect stdio
	err = mcpsync.AddServer(tmpDir, mcpsync.AddOptions{
		Name:    "local",
		Command: "uvx",
		Args:    []string{"mcp-server-fetch"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	srcFile := filepath.Join(tmpDir, mcpsync.SourceFilename)
	data, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var src mcpsync.Source
	if err := json.Unmarshal(data, &src); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if src.MCPServers["remote"].Type != "http" || src.MCPServers["remote"].URL != "https://api.example.com/mcp" {
		t.Errorf("expected auto-detected http server, got %+v", src.MCPServers["remote"])
	}
	if src.MCPServers["local"].Type != "stdio" || src.MCPServers["local"].Command != "uvx" {
		t.Errorf("expected auto-detected stdio server, got %+v", src.MCPServers["local"])
	}
}

func TestAddServer_CollisionAndValidation(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	err := mcpsync.AddServer(tmpDir, mcpsync.AddOptions{
		Name: "my-srv",
		Type: "http",
		URL:  "https://example.com/mcp",
	})
	if err != nil {
		t.Fatalf("initial add: %v", err)
	}

	// Namespace collision: my_srv vs my-srv
	err = mcpsync.AddServer(tmpDir, mcpsync.AddOptions{
		Name: "my_srv",
		Type: "http",
		URL:  "https://example.com/other",
	})
	if err == nil {
		t.Fatal("expected namespace collision error, got nil")
	}

	// Invalid name
	err = mcpsync.AddServer(tmpDir, mcpsync.AddOptions{
		Name: "invalid name!",
		Type: "http",
		URL:  "https://example.com/mcp",
	})
	if err == nil {
		t.Fatal("expected invalid name error, got nil")
	}

	// Invalid transport
	err = mcpsync.AddServer(tmpDir, mcpsync.AddOptions{
		Name: "ws-srv",
		Type: "websocket",
		URL:  "wss://example.com",
	})
	if err == nil {
		t.Fatal("expected invalid transport error, got nil")
	}
}

func TestAddServer_KeepsOAuthKeys(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	existing := `{"mcpServers":{"docs":{"type":"http","url":"https://mcp.example.com/mcp",` +
		`"oauth":{"clientId":"example-client","callbackPort":8765},` +
		`"oauthClientId":"example-client","oauthPublicClient":false,"auth":{"redirectPort":8766}}}}`
	srcFile := filepath.Join(tmpDir, mcpsync.SourceFilename)
	if err := os.WriteFile(srcFile, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	err := mcpsync.AddServer(tmpDir, mcpsync.AddOptions{Name: "tool", Command: "npx", Args: []string{"-y", "example"}})
	if err != nil {
		t.Fatalf("AddServer failed: %v", err)
	}

	data, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"type":              "http",
		"url":               "https://mcp.example.com/mcp",
		"oauth":             map[string]any{"clientId": "example-client", "callbackPort": float64(8765)},
		"oauthClientId":     "example-client",
		"oauthPublicClient": false,
		"auth":              map[string]any{"redirectPort": float64(8766)},
	}
	if diff := cmp.Diff(want, got.MCPServers["docs"]); diff != "" {
		t.Errorf("docs entry changed (-want +got):\n%s", diff)
	}
}
