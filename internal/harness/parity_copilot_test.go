package harness

// adjustCopilot edits a Copilot CLI golden (decoded) for the differences agents makes on
// purpose; every edit carries its reason.
func adjustCopilot(_ parityCase, _ string, want any) any { return want }
