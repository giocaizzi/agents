package harness

// adjustPi edits a pi golden (decoded) for the differences agents makes on
// purpose; every edit carries its reason.
func adjustPi(_ parityCase, file string, want any) any {
	if file != "settings.json" {
		return want
	}
	m, _ := want.(map[string]any)
	pkgs, _ := m["packages"].([]any)
	for i, p := range pkgs {
		// agents derives the package from its registry checkout under its data dir.
		if p == "../../.local/share/giocaizzi-harness" {
			pkgs[i] = "/home/user/.local/share/agents/registries/harness"
		}
	}
	return want
}
