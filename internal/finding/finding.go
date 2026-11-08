// Package finding is the result type of the policy and parity checks.
package finding

import (
	"fmt"
	"slices"
	"strings"
)

// Finding is one problem a check found.
type Finding struct {
	Where   string
	Message string
}

// New builds a Finding for where.
func New(where, format string, args ...any) Finding {
	return Finding{Where: where, Message: fmt.Sprintf(format, args...)}
}

func (f Finding) String() string {
	if f.Where == "" {
		return f.Message
	}
	return f.Where + ": " + f.Message
}

// Lines renders the findings one per line, sorted so the output is stable.
func Lines(fs []Finding) string {
	lines := make([]string, 0, len(fs))
	for _, f := range fs {
		lines = append(lines, f.String())
	}
	slices.Sort(lines)
	return strings.Join(slices.Compact(lines), "\n")
}
