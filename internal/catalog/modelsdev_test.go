package catalog

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseModelsDev(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/modelsdev.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseModelsDev(data, []string{"anthropic", "openai", "google", "xai"})
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(got))
	for _, m := range got {
		keys = append(keys, m.Provider+"/"+m.ID)
	}
	wantKeys := []string{
		"anthropic/claude-haiku-4-5",
		"anthropic/claude-opus-5-5",
		"google/gemini-3.1-flash-lite-preview",
		"google/gemini-4-argon",
		"google/gemma-4-31b-it",
		"openai/gpt-5.6",
		"openai/gpt-5.6-sol",
		"openai/gpt-6-luna",
		"openai/gpt-realtime-2.1",
		"xai/grok-4.7",
	}
	if diff := cmp.Diff(wantKeys, keys); diff != "" {
		t.Fatalf("models, untracked github-copilot must be absent (-want +got):\n%s", diff)
	}
	byKey := map[string]ProviderModel{}
	for _, m := range got {
		byKey[m.Provider+"/"+m.ID] = m
	}
	wantOpus := ProviderModel{
		Provider: "anthropic", ID: "claude-opus-5-5", Name: "Claude Opus 5.5",
		CanonicalID: "anthropic/claude-opus-5-5", ReleaseDate: "2026-09-22", Context: 1000000,
		Price:    &Price{In: 4, Out: 20, CacheRead: 0.2, CacheWrite: 5},
		ToolCall: true, TextOutput: true,
	}
	if diff := cmp.Diff(wantOpus, byKey["anthropic/claude-opus-5-5"]); diff != "" {
		t.Errorf("opus (-want +got):\n%s", diff)
	}
	if m := byKey["google/gemma-4-31b-it"]; m.Price != nil || m.Context != 262144 || m.CanonicalID != "google/gemma-4-31b-it" {
		t.Errorf("gemma = %+v, want null cost as nil price and the fallback canonical id", m)
	}
	if !byKey["google/gemini-3.1-flash-lite-preview"].Deprecated || byKey["google/gemini-4-argon"].Deprecated {
		t.Error("deprecated status misparsed")
	}
	if m := byKey["openai/gpt-5.6"]; m.CanonicalID != "openai/gpt-5.6-sol" {
		t.Errorf("gpt-5.6 canonical = %q", m.CanonicalID)
	}
	if rt := byKey["openai/gpt-realtime-2.1"]; !rt.ToolCall || !rt.TextOutput {
		t.Errorf("realtime = %+v, want tool call with text output", rt)
	}
}

func TestParseModelsDevDefaults(t *testing.T) {
	t.Parallel()
	data := []byte(`{
	  "openai": {"models": {"o1": {"id": "o1", "name": "O1", "modalities": {"output": ["audio"]}}}},
	  "other": {"models": {"x": {"id": "x"}}}
	}`)
	got, err := ParseModelsDev(data, []string{"openai", "absent"})
	if err != nil {
		t.Fatal(err)
	}
	want := []ProviderModel{{Provider: "openai", ID: "o1", Name: "O1", CanonicalID: "openai/o1"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestParseModelsDevInvalid(t *testing.T) {
	t.Parallel()
	if _, err := ParseModelsDev([]byte(`[`), []string{"openai"}); err == nil {
		t.Error("want error for invalid JSON")
	}
	if _, err := ParseModelsDev([]byte(`{"openai": 3}`), []string{"openai"}); err == nil {
		t.Error("want error for invalid provider")
	}
}
