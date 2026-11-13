package harness

// adjustOpenCode edits a opencode golden (decoded) for the differences agents makes on
// purpose; every edit carries its reason.
func adjustOpenCode(c parityCase, file string, want any) any {
	if c.Start != "live" || file != "opencode.jsonc" {
		return want
	}
	// The templates dropped the live permission {read **/.env deny}; the engine
	// keeps live array elements it does not manage, in their place.
	m, _ := want.(map[string]any)
	perms, _ := m["permissions"].([]any)
	if len(perms) < 1 {
		return want
	}
	kept := map[string]any{"action": "read", "effect": "deny", "resource": "**/.env"}
	m["permissions"] = append([]any{perms[0], kept}, perms[1:]...)
	return m
}
