// Package finding is the result type of the policy and parity checks.
package finding

import (
	"fmt"
	"slices"
	"strings"
)

// Severity ranks a Finding. The zero value is an error.
type Severity int

// Severities, most severe first. Only errors fail a check.
const (
	Error Severity = iota
	Warning
	Info
)

// Finding is one problem a check found.
type Finding struct {
	Where    string
	Message  string
	Severity Severity
}

// Warn builds a warning Finding for where.
func Warn(where, format string, args ...any) Finding {
	return Finding{Where: where, Message: fmt.Sprintf(format, args...), Severity: Warning}
}

// Note builds an informational Finding for where.
func Note(where, format string, args ...any) Finding {
	return Finding{Where: where, Message: fmt.Sprintf(format, args...), Severity: Info}
}

// HasErrors reports whether any finding is an error.
func HasErrors(fs []Finding) bool {
	return slices.ContainsFunc(fs, func(f Finding) bool { return f.Severity == Error })
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
