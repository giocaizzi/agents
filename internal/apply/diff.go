package apply

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const diffContext = 3

type diffOp struct {
	kind byte // ' ', '-', '+'
	text string
}

// hunk is a run of ops with its 1-based start lines in the old and new text.
type hunk struct {
	ops                []diffOp
	oldStart, newStart int
}

// unifiedDiff returns a unified diff of old and new, or "" when they are equal.
func unifiedDiff(path, oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	ops := lineDiff(splitLines(oldText), splitLines(newText))
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", path, path)
	for _, h := range hunks(ops) {
		oldCount, newCount := 0, 0
		var body strings.Builder
		for _, op := range h.ops {
			if op.kind != '+' {
				oldCount++
			}
			if op.kind != '-' {
				newCount++
			}
			body.WriteByte(op.kind)
			body.WriteString(op.text)
			if !strings.HasSuffix(op.text, "\n") {
				body.WriteString("\n\\ No newline at end of file\n")
			}
		}
		fmt.Fprintf(&sb, "@@ -%s +%s @@\n", hunkRange(h.oldStart, oldCount), hunkRange(h.newStart, newCount))
		sb.WriteString(body.String())
	}
	return sb.String()
}

// hunks groups the changed ops with diffContext lines of context; changes
// separated by more than twice the context start a new hunk.
func hunks(ops []diffOp) []hunk {
	// oldAt[i], newAt[i] are the line numbers (1-based) at ops[i].
	oldAt := make([]int, len(ops)+1)
	newAt := make([]int, len(ops)+1)
	oldAt[0], newAt[0] = 1, 1
	for i, op := range ops {
		oldAt[i+1], newAt[i+1] = oldAt[i], newAt[i]
		if op.kind != '+' {
			oldAt[i+1]++
		}
		if op.kind != '-' {
			newAt[i+1]++
		}
	}

	var out []hunk
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		start := max(i-diffContext, 0)
		last := i
		end := i
		for ; end < len(ops) && end-last <= 2*diffContext; end++ {
			if ops[end].kind != ' ' {
				last = end
			}
		}
		stop := min(last+1+diffContext, len(ops))
		out = append(out, hunk{ops: ops[start:stop], oldStart: oldAt[start], newStart: newAt[start]})
		i = stop
	}
	return out
}

func hunkRange(start, count int) string {
	switch count {
	case 0:
		return strconv.Itoa(start-1) + ",0"
	case 1:
		return strconv.Itoa(start)
	default:
		return strconv.Itoa(start) + "," + strconv.Itoa(count)
	}
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.SplitAfter(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// lcsTable returns t where t[i][j] is the longest common subsequence length of a[i:] and b[j:].
func lcsTable(a, b []string) [][]int {
	t := make([][]int, len(a)+1)
	for i := range t {
		t[i] = make([]int, len(b)+1)
	}
	for i := range slices.Backward(a) {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i][j] = t[i+1][j+1] + 1
			} else {
				t[i][j] = max(t[i+1][j], t[i][j+1])
			}
		}
	}
	return t
}

// lineDiff computes an edit script from a to b.
func lineDiff(a, b []string) []diffOp {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	lcs := lcsTable(ma, mb)

	ops := make([]diffOp, 0, len(a)+len(b))
	for _, l := range a[:pre] {
		ops = append(ops, diffOp{' ', l})
	}
	i, j := 0, 0
	for i < len(ma) || j < len(mb) {
		switch {
		case i < len(ma) && j < len(mb) && ma[i] == mb[j]:
			ops = append(ops, diffOp{' ', ma[i]})
			i++
			j++
		case i < len(ma) && (j >= len(mb) || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, diffOp{'-', ma[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', mb[j]})
			j++
		}
	}
	for _, l := range a[len(a)-suf:] {
		ops = append(ops, diffOp{' ', l})
	}
	return ops
}
