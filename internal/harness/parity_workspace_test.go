package harness

// adjustWorkspace edits a workspace golden (decoded) for the differences agents
// makes on purpose; every edit carries its reason.
func adjustWorkspace(_ parityCase, _ string, want any) any { return want }
