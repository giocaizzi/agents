package translate_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giocaizzi/agents/internal/config"
	"github.com/giocaizzi/agents/internal/translate"
)

func loadTestPolicy(t *testing.T) (*config.Policy, map[string]any) {
	t.Helper()
	f, err := os.Open("../testdata/harness-policy.toml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	p, err := config.ParseTOML(f)
	if err != nil {
		t.Fatalf("ParseTOML: %v", err)
	}

	b, err := os.ReadFile("testdata/harness-policy.golden.json")
	if err != nil {
		t.Fatal(err)
	}

	var golden map[string]any
	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatal(err)
	}
	return p, golden
}

func requireMap(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	val, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("missing or non-map key %q", key)
	}
	return val
}

func TestClaudeGolden(t *testing.T) {
	t.Parallel()
	p, golden := loadTestPolicy(t)
	linuxGolden := requireMap(t, golden, "linux")

	got := translate.ClaudeSettings(p, "linux")
	want := requireMap(t, linuxGolden, "claude")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ClaudeSettings(linux) mismatch (-want +got):\n%s", diff)
	}
}

func TestAntigravityGolden(t *testing.T) {
	t.Parallel()
	p, golden := loadTestPolicy(t)
	linuxGolden := requireMap(t, golden, "linux")

	got := translate.AntigravitySettings(p)
	want := requireMap(t, linuxGolden, "antigravity")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("AntigravitySettings mismatch (-want +got):\n%s", diff)
	}
}

func TestCopilotGolden(t *testing.T) {
	t.Parallel()
	p, golden := loadTestPolicy(t)
	linuxGolden := requireMap(t, golden, "linux")

	got := translate.CopilotSettings(p)
	want := requireMap(t, linuxGolden, "copilot")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("CopilotSettings mismatch (-want +got):\n%s", diff)
	}
}

func TestCodex(t *testing.T) {
	t.Parallel()
	p, _ := loadTestPolicy(t)
	got := translate.CodexConfig(p)
	tui, ok := got["tui"].(map[string]any)
	if !ok {
		t.Fatalf("missing tui section in codex config: %v", got)
	}
	if tui["notifications"] != true {
		t.Errorf("expected notifications=true, got %v", tui["notifications"])
	}
	if tui["notification_method"] != "bel" {
		t.Errorf("expected notification_method=bel, got %v", tui["notification_method"])
	}
}

func TestPiSettings(t *testing.T) {
	t.Parallel()
	p, _ := loadTestPolicy(t)
	got := translate.PiSettings(p)
	if got["defaultProvider"] != "openai" {
		t.Errorf("expected defaultProvider=openai, got %v", got["defaultProvider"])
	}
	if got["defaultModel"] != "gpt-5.6-terra" {
		t.Errorf("expected defaultModel=gpt-5.6-terra, got %v", got["defaultModel"])
	}

	p.Native.Pi.DefaultModel = ""
	got = translate.PiSettings(p)
	if got["defaultModel"] != "gpt-5.6-terra" {
		t.Errorf("expected defaultModel fallback=gpt-5.6-terra, got %v", got["defaultModel"])
	}
}

func TestVSCodeSettings(t *testing.T) {
	t.Parallel()
	p, _ := loadTestPolicy(t)
	got := translate.VSCodeSettings(p)
	terminal, ok := got["chat.tools.terminal.autoApprove"].(map[string]bool)
	if !ok {
		t.Fatalf("expected map[string]bool for terminalAutoApprove, got %T", got["chat.tools.terminal.autoApprove"])
	}
	if !terminal["git *"] {
		t.Errorf("expected git * to be true")
	}
	if terminal["env"] {
		t.Errorf("expected env to be false")
	}
	urls, ok := got["chat.tools.urls.autoApprove"].(map[string]bool)
	if !ok {
		t.Fatalf("expected map[string]bool for urlsAutoApprove, got %T", got["chat.tools.urls.autoApprove"])
	}
	if !urls["https://github.com/**"] {
		t.Errorf("expected https://github.com/** to be true")
	}
}

func TestVSCodeSettings_Voice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		voice        config.Voice
		wantEnabled  bool
		wantLanguage any
	}{
		{"language unset", config.Voice{Enabled: true}, true, nil},
		{"language set", config.Voice{Enabled: true, Language: "it"}, true, "it"},
		{"disabled", config.Voice{}, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := translate.VSCodeSettings(&config.Policy{Voice: tt.voice})
			if got["dictation.enabled"] != tt.wantEnabled {
				t.Errorf("dictation.enabled = %v, want %v", got["dictation.enabled"], tt.wantEnabled)
			}
			if lang := got["agents.voice.language"]; lang != tt.wantLanguage {
				t.Errorf("agents.voice.language = %v, want %v", lang, tt.wantLanguage)
			}
		})
	}
}

func TestCopilotSettings_Voice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		enabled bool
	}{
		{"enabled", true},
		{"disabled", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := translate.CopilotSettings(&config.Policy{Voice: config.Voice{Enabled: tt.enabled}})
			want := map[string]any{"enabled": tt.enabled}
			if diff := cmp.Diff(want, got["voice"]); diff != "" {
				t.Errorf("voice mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestClaudeSettings_VoiceModeDefault(t *testing.T) {
	t.Parallel()
	got := translate.ClaudeSettings(&config.Policy{Voice: config.Voice{Enabled: true}}, "linux")
	want := map[string]any{"enabled": true, "mode": "hold", "autoSubmit": false}
	if diff := cmp.Diff(want, got["voice"]); diff != "" {
		t.Errorf("voice mismatch (-want +got):\n%s", diff)
	}
}
