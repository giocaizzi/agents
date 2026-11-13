package mcpsync_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	toml "github.com/pelletier/go-toml/v2"

	"github.com/giocaizzi/agents/internal/apply"
	"github.com/giocaizzi/agents/internal/mcpsync"
)

func TestDocs(t *testing.T) {
	t.Parallel()

	servers := map[string]mcpsync.ServerEntry{
		"web": {
			Type: "http", URL: "https://a.example.com", Headers: map[string]string{"X-Key": "v"},
			OAuth: &mcpsync.OAuth{ClientID: "c1", CallbackPort: 8765}, OAuthClientID: "c1",
			OAuthPublicClient: new(true), Auth: &mcpsync.Auth{RedirectPort: 8765},
		},
		"local": {Type: "stdio", Command: "npx", Args: []string{"-y", "srv"}, Env: map[string]string{"K": "v"}},
	}
	tests := []struct {
		harness string
		relPath string
		format  apply.Format
		want    map[string]any
	}{
		{"claude", ".mcp.json", apply.JSON, map[string]any{"mcpServers": map[string]any{
			"web": map[string]any{
				"type": "http", "url": "https://a.example.com", "headers": map[string]any{"X-Key": "v"},
				"oauth":         map[string]any{"clientId": "c1", "callbackPort": 8765},
				"oauthClientId": "c1", "oauthPublicClient": true, "auth": map[string]any{"redirectPort": 8765},
			},
			"local": map[string]any{"type": "stdio", "command": "npx", "args": []any{"-y", "srv"}, "env": map[string]any{"K": "v"}},
		}}},
		{"codex", ".codex/config.toml", apply.TOML, map[string]any{"mcp_servers": map[string]any{
			"web": map[string]any{
				"url": "https://a.example.com", "http_headers": map[string]any{"X-Key": "v"},
				"oauth": map[string]any{"client_id": "c1", "callback_port": 8765},
			},
			"local": map[string]any{"command": "npx", "args": []any{"-y", "srv"}, "env": map[string]any{"K": "v"}},
		}}},
		{"pi", ".pi/mcp.json", apply.JSON, map[string]any{"mcpServers": map[string]any{
			"web": map[string]any{
				"type": "http", "url": "https://a.example.com", "headers": map[string]any{"X-Key": "v"},
				"oauth": map[string]any{"clientId": "c1", "callbackPort": 8765},
			},
			"local": map[string]any{"type": "stdio", "command": "npx", "args": []any{"-y", "srv"}, "env": map[string]any{"K": "v"}},
		}}},
		{"antigravity", ".agents/mcp_config.json", apply.JSON, map[string]any{"mcpServers": map[string]any{
			"web": map[string]any{
				"type": "http", "url": "https://a.example.com", "headers": map[string]any{"X-Key": "v"},
				"oauth": map[string]any{"clientId": "c1"},
			},
			"local": map[string]any{"type": "stdio", "command": "npx", "args": []any{"-y", "srv"}, "env": map[string]any{"K": "v"}},
		}}},
		{"opencode", "opencode.json", apply.JSON, map[string]any{
			"$schema": "https://opencode.ai/config.json",
			"mcp": map[string]any{"servers": map[string]any{
				"web": map[string]any{
					"type": "remote", "url": "https://a.example.com", "headers": map[string]any{"X-Key": "v"},
					"oauth": map[string]any{"client_id": "c1", "callback_port": 8765},
				},
				"local": map[string]any{"type": "local", "command": []any{"npx", "-y", "srv"}, "environment": map[string]any{"K": "v"}},
			}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.harness, func(t *testing.T) {
			t.Parallel()
			docs := mcpsync.Docs(servers, []string{tt.harness})
			if len(docs) != 1 {
				t.Fatalf("got %d docs, want 1", len(docs))
			}
			d := docs[0]
			if d.Harness != tt.harness || d.RelPath != tt.relPath || d.Format != tt.format {
				t.Errorf("doc = %s %s %v, want %s %s %v", d.Harness, d.RelPath, d.Format, tt.harness, tt.relPath, tt.format)
			}
			if diff := cmp.Diff(tt.want, d.Managed); diff != "" {
				t.Errorf("managed (-want +got):\n%s", diff)
			}
			// The managed document must render as the format says.
			if tt.format == apply.TOML {
				if _, err := toml.Marshal(d.Managed); err != nil {
					t.Errorf("toml: %v", err)
				}
			} else if _, err := json.Marshal(d.Managed); err != nil {
				t.Errorf("json: %v", err)
			}
		})
	}
}

func TestDocs_SharedFileAndUnknown(t *testing.T) {
	t.Parallel()

	servers := map[string]mcpsync.ServerEntry{"a": {Type: "http", URL: "https://a.example.com"}}
	docs := mcpsync.Docs(servers, []string{"claude", "copilot", "vscode", "nope"})
	if len(docs) != 3 {
		t.Fatalf("got %d docs, want one per known harness", len(docs))
	}
	for _, d := range docs {
		if d.RelPath != ".mcp.json" {
			t.Errorf("%s: path %s, want .mcp.json", d.Harness, d.RelPath)
		}
		if _, ok := d.Managed["_generatedBy"]; ok {
			t.Errorf("%s: managed document has a generated-by marker", d.Harness)
		}
	}
}
