package harness

// adjustOpenCode edits a opencode golden (decoded) for the differences agents makes on
// purpose; every edit carries its reason.
func adjustOpenCode(_ parityCase, _ string, want any) any { return want }
