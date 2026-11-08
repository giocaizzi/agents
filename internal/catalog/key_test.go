package catalog

import "testing"

func TestKey(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"Claude Opus 5.5 (High)", "claude-opus-5-5"},
		{"claude-opus-5-5", "claude-opus-5-5"},
		{"claude-opus-5.5", "claude-opus-5-5"},
		{"anthropic/claude-opus-5-5", "anthropic/claude-opus-5-5"},
		{"GPT-6.1 Sol", "gpt-6-1-sol"},
		{"gpt-6.1-sol", "gpt-6-1-sol"},
		{"gpt-6.1-sol_max", "gpt-6-1-sol"},
		{"gpt-5-2025-08-07_high", "gpt-5"},
		{"claude-sonnet-4-5-20250929", "claude-sonnet-4-5"},
		{"claude-sonnet-4-5-20250929_unknown", "claude-sonnet-4-5"},
		{"Claude Haiku 4.5 (latest)", "claude-haiku-4-5"},
		{"  GPT-6 Astra (Max)  ", "gpt-6-astra"},
		{"gpt  5..mini", "gpt-5-mini"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := Key(tt.in); got != tt.want {
				t.Errorf("Key(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func FuzzKey(f *testing.F) {
	for _, s := range []string{"Claude Opus 5.5 (High)", "gpt-5-2025-08-07_high", "a (b)_c 2025-08-07", "x_", "-20250101", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		k := Key(s)
		if again := Key(k); again != k {
			t.Errorf("Key not idempotent: Key(%q) = %q, Key of that = %q", s, k, again)
		}
	})
}
