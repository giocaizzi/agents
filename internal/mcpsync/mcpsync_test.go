package mcpsync_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/mcpsync"
)

func TestParseAndValidate_Valid(t *testing.T) {
	t.Parallel()

	input := `{
		"mcpServers": {
			"server_a": {
				"type": "http",
				"url": "https://example.com/sse"
			},
			"server-b": {
				"type": "http",
				"url": "https://acme-${MCP_CONNECTOR_ACCOUNT}.domain.com/mcp"
			}
		}
	}`

	src, err := mcpsync.ParseAndValidate(strings.NewReader(input), "team-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(src.MCPServers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(src.MCPServers))
	}

	entryB := src.MCPServers["server-b"]
	expectedURL := "https://acme-team-123.domain.com/mcp"
	if entryB.URL != expectedURL {
		t.Errorf("url placeholder replacement mismatch: got %q, want %q", entryB.URL, expectedURL)
	}
}

func TestParseAndValidate_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := mcpsync.ParseAndValidate(strings.NewReader(`{invalid json`), "acc")
	if err == nil {
		t.Fatal("expected error on invalid JSON, got nil")
	}
}

func TestParseAndValidate_ExtraTopLevelFields(t *testing.T) {
	t.Parallel()

	input := `{
		"mcpServers": {},
		"extraField": "not allowed"
	}`

	_, err := mcpsync.ParseAndValidate(strings.NewReader(input), "acc")
	if err == nil {
		t.Fatal("expected error on extra top-level field, got nil")
	}
}

func TestParseAndValidate_ExtraEntryFields(t *testing.T) {
	t.Parallel()

	input := `{
		"mcpServers": {
			"srv": {
				"type": "http",
				"url": "https://example.com/mcp",
				"unknownField": "not allowed"
			}
		}
	}`

	_, err := mcpsync.ParseAndValidate(strings.NewReader(input), "acc")
	if err == nil {
		t.Fatal("expected error on extra entry field, got nil")
	}
}

func TestParseAndValidate_Headers(t *testing.T) {
	t.Parallel()

	input := `{
		"mcpServers": {
			"srv": {
				"type": "http",
				"url": "https://example.com/mcp",
				"headers": {"Authorization": "Bearer token"}
			}
		}
	}`

	src, err := mcpsync.ParseAndValidate(strings.NewReader(input), "acc")
	if err != nil {
		t.Fatalf("unexpected error with headers: %v", err)
	}

	entry := src.MCPServers["srv"]
	if entry.Headers["Authorization"] != "Bearer token" {
		t.Errorf("expected header value %q, got %q", "Bearer token", entry.Headers["Authorization"])
	}
}

func TestParseAndValidate_Stdio(t *testing.T) {
	t.Parallel()

	input := `{
		"mcpServers": {
			"local_runner": {
				"type": "stdio",
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-everything"],
				"env": {
					"DEBUG": "true"
				}
			}
		}
	}`

	src, err := mcpsync.ParseAndValidate(strings.NewReader(input), "acc")
	if err != nil {
		t.Fatalf("unexpected error with stdio transport: %v", err)
	}

	entry := src.MCPServers["local_runner"]
	if entry.Command != "npx" {
		t.Errorf("expected command 'npx', got %q", entry.Command)
	}
	if len(entry.Args) != 2 || entry.Args[0] != "-y" {
		t.Errorf("unexpected args: %v", entry.Args)
	}
	if entry.Env["DEBUG"] != "true" {
		t.Errorf("expected env DEBUG=true, got %q", entry.Env["DEBUG"])
	}
}

func TestParseAndValidate_InvalidServerNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		serverName string
	}{
		{"spaces", "server name"},
		{"symbols", "server@name"},
		{"slash", "server/name"},
		{"dots", "server.name"},
		{"empty", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := `{
				"mcpServers": {
					"` + tc.serverName + `": {
						"type": "http",
						"url": "https://example.com"
					}
				}
			}`
			_, err := mcpsync.ParseAndValidate(strings.NewReader(input), "acc")
			if err == nil {
				t.Fatalf("expected error for server name %q, got nil", tc.serverName)
			}
		})
	}
}

func TestParseAndValidate_NamespaceCollision(t *testing.T) {
	t.Parallel()

	input := `{
		"mcpServers": {
			"foo-bar": {
				"type": "http",
				"url": "https://example.com"
			},
			"foo_bar": {
				"type": "http",
				"url": "https://example.com"
			}
		}
	}`

	_, err := mcpsync.ParseAndValidate(strings.NewReader(input), "acc")
	if err == nil {
		t.Fatal("expected namespace collision error, got nil")
	}
	if !strings.Contains(err.Error(), "namespace collision") {
		t.Fatalf("expected error containing 'namespace collision', got: %v", err)
	}
}

func TestParseAndValidate_InvalidStdioWithURL(t *testing.T) {
	t.Parallel()

	input := `{
		"mcpServers": {
			"srv": {
				"type": "stdio",
				"url": "https://example.com"
			}
		}
	}`

	_, err := mcpsync.ParseAndValidate(strings.NewReader(input), "acc")
	if err == nil {
		t.Fatal("expected error on stdio with url, got nil")
	}
}

func TestParseAndValidate_UnsupportedTransport(t *testing.T) {
	t.Parallel()

	input := `{
		"mcpServers": {
			"srv": {
				"type": "websocket",
				"url": "wss://example.com"
			}
		}
	}`

	_, err := mcpsync.ParseAndValidate(strings.NewReader(input), "acc")
	if err == nil {
		t.Fatal("expected error on unsupported transport, got nil")
	}
}

func TestParseAndValidate_InvalidURLs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		url  string
	}{
		{"ftp", "ftp://example.com"},
		{"no host", "http:///path"},
		{"userinfo", "https://user:pass@example.com/mcp"},
		{"query param", "https://example.com/mcp?foo=bar"},
		{"fragment", "https://example.com/mcp#section"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := `{
				"mcpServers": {
					"srv": {
						"type": "http",
						"url": "` + tc.url + `"
					}
				}
			}`
			_, err := mcpsync.ParseAndValidate(strings.NewReader(input), "acc")
			if err == nil {
				t.Fatalf("expected error for url %q, got nil", tc.url)
			}
		})
	}
}

func TestPlaceholder_Validation(t *testing.T) {
	t.Parallel()

	t.Run("disallowed placeholder", func(t *testing.T) {
		t.Parallel()
		input := `{
			"mcpServers": {
				"srv": {
					"type": "http",
					"url": "https://${UNSUPPORTED_VAR}.com"
				}
			}
		}`
		_, err := mcpsync.ParseAndValidate(strings.NewReader(input), "acc")
		if err == nil {
			t.Fatal("expected error for unsupported placeholder, got nil")
		}
	})

	t.Run("empty account parameter when referenced", func(t *testing.T) {
		t.Parallel()
		input := `{
			"mcpServers": {
				"srv": {
					"type": "http",
					"url": "https://${MCP_CONNECTOR_ACCOUNT}.com"
				}
			}
		}`
		_, err := mcpsync.ParseAndValidate(strings.NewReader(input), "")
		if err == nil {
			t.Fatal("expected error for empty account parameter, got nil")
		}
	})

	t.Run("invalid characters in account parameter", func(t *testing.T) {
		t.Parallel()
		input := `{
			"mcpServers": {
				"srv": {
					"type": "http",
					"url": "https://${MCP_CONNECTOR_ACCOUNT}.com"
				}
			}
		}`
		_, err := mcpsync.ParseAndValidate(strings.NewReader(input), "bad_account#name")
		if err == nil {
			t.Fatal("expected error for invalid characters in account, got nil")
		}
	})
}

func TestRender_DeterminismAndOutputs(t *testing.T) {
	t.Parallel()

	servers := map[string]mcpsync.ServerEntry{
		"z-server": {Type: "http", URL: "https://z.example.com"},
		"a-server": {Type: "http", URL: "https://a.example.com"},
		"m-server": {Type: "http", URL: "https://m.example.com"},
	}

	codex := mcpsync.RenderCodex(servers)
	expectedCodex := `# Generated by agents mcp sync; edit .mcp.json and run agents mcp sync.

[mcp_servers."a-server"]
url = "https://a.example.com"

[mcp_servers."m-server"]
url = "https://m.example.com"

[mcp_servers."z-server"]
url = "https://z.example.com"
`
	if diff := cmp.Diff(expectedCodex, string(codex)); diff != "" {
		t.Errorf("RenderCodex mismatch (-want +got):\n%s", diff)
	}

	pi, err := mcpsync.RenderPi(servers)
	if err != nil {
		t.Fatalf("RenderPi failed: %v", err)
	}

	var parsedPi struct {
		GeneratedBy string                         `json:"_generatedBy"`
		MCPServers  map[string]mcpsync.ServerEntry `json:"mcpServers"`
	}
	if err := json.Unmarshal(pi, &parsedPi); err != nil {
		t.Fatalf("failed to unmarshal Pi JSON: %v", err)
	}

	if parsedPi.GeneratedBy != mcpsync.CurrentMarker {
		t.Errorf("Pi generatedBy mismatch: got %q, want %q", parsedPi.GeneratedBy, mcpsync.CurrentMarker)
	}

	ag := mcpsync.RenderAntigravity(pi)
	if diff := cmp.Diff(string(pi), string(ag)); diff != "" {
		t.Errorf("Antigravity config mismatch with Pi (-want +got):\n%s", diff)
	}
}

func TestPreflight_SymlinksAndOwnership(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// 1. Existing unowned toml file
	unownedToml := filepath.Join(tmpDir, "unowned.toml")
	if err := os.WriteFile(unownedToml, []byte("some = \"content\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mcpsync.PreflightCheck(tmpDir, unownedToml); err == nil {
		t.Fatal("expected error on unowned toml file, got nil")
	}

	// 2. Existing owned toml file with current marker
	ownedToml := filepath.Join(tmpDir, "owned.toml")
	if err := os.WriteFile(ownedToml, []byte("# Generated by "+mcpsync.CurrentMarker+"\n[sec]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mcpsync.PreflightCheck(tmpDir, ownedToml); err != nil {
		t.Fatalf("unexpected error on owned toml: %v", err)
	}

	// 3. Existing owned toml file with legacy marker
	legacyToml := filepath.Join(tmpDir, "legacy.toml")
	if err := os.WriteFile(legacyToml, []byte("# Generated by "+mcpsync.LegacyMarker+"\n[sec]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mcpsync.PreflightCheck(tmpDir, legacyToml); err != nil {
		t.Fatalf("unexpected error on legacy toml: %v", err)
	}

	// 4. Existing owned JSON file
	ownedJSON := filepath.Join(tmpDir, "owned.json")
	if err := os.WriteFile(ownedJSON, []byte(`{"_generatedBy":"`+mcpsync.CurrentMarker+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mcpsync.PreflightCheck(tmpDir, ownedJSON); err != nil {
		t.Fatalf("unexpected error on owned json: %v", err)
	}

	// 5. Symlink target rejection
	symlinkTarget := filepath.Join(tmpDir, "symlink_target.json")
	if err := os.Symlink(ownedJSON, symlinkTarget); err != nil {
		t.Fatal(err)
	}
	if err := mcpsync.PreflightCheck(tmpDir, symlinkTarget); err == nil {
		t.Fatal("expected error on symlink target, got nil")
	}
}

func TestSyncAndCheck_EndToEnd(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	srcFile := filepath.Join(tmpDir, mcpsync.SourceFilename)
	sourceContent := `{
		"mcpServers": {
			"my-server": {
				"type": "http",
				"url": "https://${MCP_CONNECTOR_ACCOUNT}.example.com/mcp"
			}
		}
	}`
	if err := os.WriteFile(srcFile, []byte(sourceContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// Check succeeds without writing
	if _, err := mcpsync.Check(tmpDir, "acct"); err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// Verify target files do not exist yet
	codexPath := filepath.Join(tmpDir, mcpsync.CodexTargetRelPath)
	if _, err := os.Stat(codexPath); !os.IsNotExist(err) {
		t.Fatalf("expected %s not to exist after Check", codexPath)
	}

	// Sync writes all files with mode 0o600
	outputs, _, err := mcpsync.Sync(tmpDir, "acct")
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if len(outputs) != 3 {
		t.Fatalf("expected 3 output files, got %d", len(outputs))
	}

	for _, out := range outputs {
		fi, err := os.Stat(out.Path)
		if err != nil {
			t.Fatalf("stat failed on %s: %v", out.Path, err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("file %s has mode %o, want 0600", out.Path, fi.Mode().Perm())
		}
	}
}
