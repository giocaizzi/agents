package cli_test

import "testing"

// hermeticGetenv is a getenv over a map: HOME and USERPROFILE are a fresh
// temporary directory and XDG_* is unset.
func hermeticGetenv(t *testing.T) func(string) string {
	t.Helper()
	home := t.TempDir()
	env := map[string]string{"HOME": home, "USERPROFILE": home}
	return func(k string) string { return env[k] }
}
