package apply

import "io/fs"

// Format is the on-disk syntax of a Target.
type Format int

// Supported formats.
const (
	JSON Format = iota
	JSONC
	TOML
	Text
)

// formats lists every Format in the order of its manifest name.
func formats() []Format { return []Format{JSON, JSONC, TOML, Text} }

func formatName(f Format) string {
	switch f {
	case JSON:
		return "json"
	case JSONC:
		return "jsonc"
	case TOML:
		return "toml"
	case Text:
		return "text"
	}
	return ""
}

func parseFormat(name string) (Format, bool) {
	for _, f := range formats() {
		if formatName(f) == name {
			return f, true
		}
	}
	return 0, false
}

// Target is one native file and the keys agents manages in it.
type Target struct {
	Owner   string     // harness name, for messages only
	Path    string     // absolute
	Format  Format     // syntax of the file
	Managed any        // JSON/JSONC/TOML: map[string]any or *Object (ordered); Text: string (whole file content)
	Union   [][]string // key paths whose arrays merge as a union (live elements kept, managed ones appended when new)
	// Legacy lists key paths of a known earlier manager, deleted only on the
	// first apply of this path (no manifest entry yet): a one-time migration.
	// Every later deletion comes from the manifest, never from this list.
	Legacy [][]string
	// Sensitive holds expanded secret values this target carries; diffs
	// print them redacted.
	Sensitive []string
	Mode      fs.FileMode // mode for a new file; 0 means 0o600
	CheckOnly bool        // never written: compared only (drift)
}

// Status is the outcome for one Target.
type Status string

// Possible statuses.
const (
	StatusCreate    Status = "create"
	StatusUpdate    Status = "update"
	StatusDelete    Status = "delete" // a file agents made and no longer produces
	StatusUnchanged Status = "unchanged"
	StatusDrift     Status = "drift"   // CheckOnly target differs, or a file agents wrote was edited and is left alone
	StatusRefused   Status = "refused" // symlink target that is not CheckOnly
)

// Result reports what happened to one Target. Results of files that the
// manifest lists but the run no longer produces follow the targets' results.
type Result struct {
	Target Target
	Status Status
	Diff   string // unified diff of the change; set only when diffs were requested and the file changes
	Err    error
}
