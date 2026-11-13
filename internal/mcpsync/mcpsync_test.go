package mcpsync_test

import (
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

func parseOne(t *testing.T, entry string) (mcpsync.ServerEntry, error) {
	t.Helper()
	src, err := mcpsync.ParseAndValidate(strings.NewReader(`{"mcpServers":{"s":`+entry+`}}`), "acct")
	if err != nil {
		return mcpsync.ServerEntry{}, err
	}
	return src.MCPServers["s"], nil
}

func TestParseAndValidate_OAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entry   string
		wantErr string
		want    mcpsync.ServerEntry
	}{
		{
			name:  "nested only",
			entry: `{"url":"https://a.example.com","oauth":{"clientId":"example-client","callbackPort":8765}}`,
			want: mcpsync.ServerEntry{
				Type: "http", URL: "https://a.example.com",
				OAuth: &mcpsync.OAuth{ClientID: "example-client", CallbackPort: 8765},
			},
		},
		{
			name:  "flat only",
			entry: `{"url":"https://a.example.com","oauthClientId":"example-client","oauthPublicClient":true,"auth":{"redirectPort":8766}}`,
			want: mcpsync.ServerEntry{
				Type: "http", URL: "https://a.example.com",
				OAuthClientID: "example-client", OAuthPublicClient: new(true), Auth: &mcpsync.Auth{RedirectPort: 8766},
			},
		},
		{
			name:  "both styles kept verbatim",
			entry: `{"url":"https://a.example.com","oauth":{"clientId":"nested-client"},"auth":{"redirectPort":8766}}`,
			want: mcpsync.ServerEntry{
				Type: "http", URL: "https://a.example.com",
				OAuth: &mcpsync.OAuth{ClientID: "nested-client"}, Auth: &mcpsync.Auth{RedirectPort: 8766},
			},
		},
		{
			name:  "port 0 is unset",
			entry: `{"url":"https://a.example.com","oauth":{"callbackPort":0},"auth":{"redirectPort":0}}`,
			want: mcpsync.ServerEntry{
				Type: "http", URL: "https://a.example.com",
				OAuth: &mcpsync.OAuth{}, Auth: &mcpsync.Auth{},
			},
		},
		{
			name:    "stdio with oauth",
			entry:   `{"command":"npx","oauth":{"clientId":"example-client"}}`,
			wantErr: `server "s": oauth, oauthClientId, oauthPublicClient and auth not allowed for stdio transport`,
		},
		{
			name:    "stdio with flat client id",
			entry:   `{"command":"npx","oauthClientId":"example-client"}`,
			wantErr: "not allowed for stdio transport",
		},
		{
			name:    "oauth port too large",
			entry:   `{"url":"https://a.example.com","oauth":{"callbackPort":70000}}`,
			wantErr: "oauth.callbackPort",
		},
		{
			name:    "oauth port negative",
			entry:   `{"url":"https://a.example.com","oauth":{"callbackPort":-1}}`,
			wantErr: "oauth.callbackPort",
		},
		{
			name:    "auth port too large",
			entry:   `{"url":"https://a.example.com","auth":{"redirectPort":70000}}`,
			wantErr: "auth.redirectPort",
		},
		{
			name:    "client secret is unknown",
			entry:   `{"url":"https://a.example.com","oauth":{"clientId":"example-client","clientSecret":"x"}}`,
			wantErr: "clientSecret",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseOne(t, tt.entry)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("entry mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWarnings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		servers map[string]mcpsync.ServerEntry
		want    []string
	}{
		{
			name: "port set, sorted by name",
			servers: map[string]mcpsync.ServerEntry{
				"zed":   {Type: "http", Auth: &mcpsync.Auth{RedirectPort: 9000}},
				"alpha": {Type: "http", OAuth: &mcpsync.OAuth{CallbackPort: 8765}},
			},
			want: []string{
				`antigravity: server "alpha": callback port 8765 not supported, left out (register https://antigravity.google/oauth-callback as a redirect URI)`,
				`antigravity: server "zed": callback port 9000 not supported, left out (register https://antigravity.google/oauth-callback as a redirect URI)`,
			},
		},
		{
			name: "public client id only",
			servers: map[string]mcpsync.ServerEntry{
				"a": {Type: "http", OAuth: &mcpsync.OAuth{ClientID: "example-client"}, OAuthPublicClient: new(true)},
			},
			want: nil,
		},
		{
			name: "confidential client, flag false or unset",
			servers: map[string]mcpsync.ServerEntry{
				"unset": {Type: "http", OAuth: &mcpsync.OAuth{ClientID: "example-client"}},
				"false": {Type: "http", OAuthClientID: "example-client", OAuthPublicClient: new(false)},
				"none":  {Type: "http", URL: "https://a.example.com"},
			},
			want: []string{
				`server "false": OAuth client is not public, so its secret is not synced; add the secret through the harness's own login`,
				`server "unset": OAuth client is not public, so its secret is not synced; add the secret through the harness's own login`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, mcpsync.Warnings(tt.servers)); diff != "" {
				t.Errorf("warnings mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseAndValidate_OAuthClientIDRejectsEscapes(t *testing.T) {
	t.Parallel()

	for _, id := range []string{`a\u0007b`, `a\u007fb`, `a\"b`, `a\\b`, `a\nb`} {
		for _, style := range []string{`"oauth":{"clientId":"` + id + `"}`, `"oauthClientId":"` + id + `"`} {
			t.Run(style, func(t *testing.T) {
				t.Parallel()
				_, err := mcpsync.ParseAndValidate(strings.NewReader(
					`{"mcpServers":{"s":{"url":"https://a.example.com",`+style+`}}}`), "acct")
				if err == nil || !strings.Contains(err.Error(), "oauth client id must not contain") {
					t.Errorf("error = %v, want a client id error", err)
				}
			})
		}
	}
}

// Rendering escapes any string, so the Codex file stays valid TOML even for
// values that ParseAndValidate does not reject.
