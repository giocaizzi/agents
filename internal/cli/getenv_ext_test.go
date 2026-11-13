package cli_test

import "testing"

// hermeticGetenv is a getenv over a map: HOME and USERPROFILE are a fresh
// temporary directory, XDG_* is unset and extra overrides or adds variables.
func hermeticGetenv(t *testing.T, extra map[string]string) func(string) string {
	t.Helper()
	home := t.TempDir()
	env := map[string]string{"HOME": home, "USERPROFILE": home}
	for k, v := range extra {
		env[k] = v
	}
	return func(k string) string { return env[k] }
}
