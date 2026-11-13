package apply

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	diffContext = 3
	removedMark = "«removed»"
)

type diffOp struct {
	kind byte // ' ', '-', '+'
	text string
}

// hunk is a run of ops with its 1-based start lines in the old and new text.
type hunk struct {
	ops                []diffOp
	oldStart, newStart int
}

// buildDiff returns the diff of a change to t, safe to print: values of keys
// agents does not manage show as «unmanaged» and every secret of the target
// as «redacted». It never falls back to the raw texts.
func buildDiff(t Target, managed [][]string, oldText, newText string) string {
	a, b := oldText, newText
	if t.Format != Text {
		isManaged := func(path []string) bool {
			for _, m := range managed {
				if hasPrefixPath(path, m) {
					return true
				}
			}
			return false
		}
		var errA, errB error
		a, errA = maskText(t.Format, oldText, isManaged)
		b, errB = maskText(t.Format, newText, isManaged)
		if errA != nil || errB != nil {
			return "(diff unavailable: the file could not be scanned)\n"
		}
	}
	secrets := append(slices.Clone(t.Sensitive), rotatedSecrets(t, oldText)...)
	retired := retiredValues(t, managed, oldText, newText)
	return unifiedDiff(t.Path, a, b, func(kind byte, line string) string {
		line = redactWith(line, secrets, redactedMark)
		if kind == '-' {
			line = maskRetired(line, retired)
		}
		return line
	})
}

// rotatedSecrets returns the live values, in oldText, of the managed string
// keys whose new value carries a secret: the previous token of a rotated
// secret is no longer in Sensitive but must not be printed on a - line.
func rotatedSecrets(t Target, oldText string) []string {
	if t.Format == Text || len(t.Sensitive) == 0 {
		return nil
	}
	live, err := parseLive(t.Format, oldText)
	if err != nil {
		return nil
	}
	managed, err := normalize(t.Managed)
	if err != nil {
		return nil
	}
	mo, ok := managed.(*Object)
	if !ok {
		return nil
	}
	var out []string
	collectRotated(mo, live, func(v string) bool {
		return slices.ContainsFunc(t.Sensitive, func(s string) bool { return s != "" && strings.Contains(v, s) })
	}, &out)
	return out
}

// collectRotated descends the managed value m and the live value l together,
// through objects and arrays (by index), and appends the live string wherever
// the managed one carries a secret.
func collectRotated(m, l any, hasSecret func(string) bool, out *[]string) {
	switch v := m.(type) {
	case *Object:
		if lo, ok := l.(*Object); ok {
			for _, k := range v.Keys {
				collectRotated(v.Values[k], lo.Values[k], hasSecret, out)
			}
		}
	case []any:
		if la, ok := l.([]any); ok {
			for i := 0; i < len(v) && i < len(la); i++ {
				collectRotated(v[i], la[i], hasSecret, out)
			}
		}
	case string:
		if old, ok := l.(string); ok && old != "" && hasSecret(v) {
			*out = append(*out, old)
		}
	}
}

// maskRetired replaces, on a removed line, each retired value written as a
// whole JSON or TOML string (quoted, raw or escaped) with «removed». Matching
// the quoted string keeps a short value from altering other text on the line.
func maskRetired(line string, retired []string) string {
	var args []string
	for _, v := range retired {
		toml := tomlString(v)
		forms := []string{`"` + jsonInner(v, false) + `"`, `"` + jsonInner(v, true) + `"`, toml}
		for _, f := range forms {
			args = append(args, f, `"`+removedMark+`"`)
		}
	}
	if len(args) == 0 {
		return line
	}
	return strings.NewReplacer(args...).Replace(line)
}

// retiredValues returns the string leaves that managed paths held in oldText
// and no longer hold under a managed path in newText (an unmanaged leaf
// prints as «unmanaged», so it does not keep a value hidden). A key dropped from the config (an
// MCP server or header removed) leaves its secret out of Sensitive, and the
// manifest holds no values, so every retired managed string is masked on the
// - line that held it, whatever its path.
func retiredValues(t Target, managed [][]string, oldText, newText string) []string {
	if t.Format == Text {
		return nil
	}
	oldDoc, errO := parseLive(t.Format, oldText)
	newDoc, errN := parseLive(t.Format, newText)
	if errO != nil || errN != nil {
		return nil
	}
	kept := map[string]bool{}
	for _, p := range managed {
		for _, s := range stringLeaves(getPath(newDoc, p), nil) {
			kept[s] = true
		}
	}
	var out []string
	for _, p := range managed {
		for _, s := range stringLeaves(getPath(oldDoc, p), nil) {
			if !kept[s] {
				out = append(out, s)
			}
		}
	}
	return out
}

// stringLeaves appends every non-empty string under v to out.
func stringLeaves(v any, out []string) []string {
	switch x := v.(type) {
	case string:
		if x != "" {
			out = append(out, x)
		}
	case []any:
		for _, e := range x {
			out = stringLeaves(e, out)
		}
	case *Object:
		for _, k := range x.Keys {
			out = stringLeaves(x.Values[k], out)
		}
	}
	return out
}

// maskText replaces the values of unmanaged keys in text with a marker.
func maskText(f Format, text string, managed func([]string) bool) (string, error) {
	if strings.TrimSpace(text) == "" {
		return text, nil
	}
	if f == TOML {
		return maskTOML(text, managed)
	}
	return maskJSON(text, managed)
}

// Redact replaces every secret in text, as written raw or escaped for JSON
// (with and without HTML escaping), TOML or a URL, with «redacted».
func Redact(text string, secrets []string) string {
	return redact(text, secrets)
}

func redact(text string, secrets []string) string {
	return redactWith(text, secrets, redactedMark)
}

func redactWith(text string, secrets []string, mark string) string {
	var pairs []string
	for _, s := range secrets {
		if s == "" {
			continue
		}
		pairs = append(pairs, s)
		toml := tomlString(s)
		forms := []string{
			jsonInner(s, false), jsonInner(s, true), toml[1 : len(toml)-1],
			url.QueryEscape(s), url.PathEscape(s),
		}
		for _, f := range forms {
			if f != s && f != "" {
				pairs = append(pairs, f)
			}
		}
	}
	if len(pairs) == 0 {
		return text
	}
	// Longest first, so a secret that contains another is replaced whole.
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && len(pairs[j]) > len(pairs[j-1]); j-- {
			pairs[j], pairs[j-1] = pairs[j-1], pairs[j]
		}
	}
	args := make([]string, 0, 2*len(pairs))
	for _, p := range pairs {
		args = append(args, p, mark)
	}
	return strings.NewReplacer(args...).Replace(text)
}

// jsonInner is s as the inside of a JSON string, with HTML escaping or without.
func jsonInner(s string, html bool) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(html)
	if err := enc.Encode(s); err != nil {
		return s
	}
	q := strings.TrimRight(buf.String(), "\n")
	return q[1 : len(q)-1]
}

// unifiedDiff returns a unified diff of old and new, or "" when they are equal.
// mask, when set, rewrites each content line given its op kind; the file and hunk header lines
// are never passed through it.
func unifiedDiff(path, oldText, newText string, mask func(kind byte, line string) string) string {
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
			text := op.text
			if mask != nil {
				text = mask(op.kind, text)
			}
			body.WriteByte(op.kind)
			body.WriteString(text)
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

// lineDiff computes an edit script from a to b with Myers' O(ND) algorithm in
// linear space: it finds the middle of the shortest edit script and recurses
// on both halves.
func lineDiff(a, b []string) []diffOp {
	return diffRange(make([]diffOp, 0, len(a)+len(b)), a, b)
}

func diffRange(ops []diffOp, a, b []string) []diffOp {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	for _, l := range a[:pre] {
		ops = append(ops, diffOp{' ', l})
	}
	a, b = a[pre:], b[pre:]
	suf := 0
	for suf < len(a) && suf < len(b) && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ma, mb := a[:len(a)-suf], b[:len(b)-suf]
	switch {
	case len(ma) == 0:
		for _, l := range mb {
			ops = append(ops, diffOp{'+', l})
		}
	case len(mb) == 0:
		for _, l := range ma {
			ops = append(ops, diffOp{'-', l})
		}
	default:
		x, y, ok := middleSnake(ma, mb)
		if !ok {
			for _, l := range ma {
				ops = append(ops, diffOp{'-', l})
			}
			for _, l := range mb {
				ops = append(ops, diffOp{'+', l})
			}
			break
		}
		ops = diffRange(ops, ma[:x], mb[:y])
		ops = diffRange(ops, ma[x:], mb[y:])
	}
	for _, l := range a[len(a)-suf:] {
		ops = append(ops, diffOp{' ', l})
	}
	return ops
}

// pass is one direction of the middle-snake search: v[off+k] is the furthest
// x reached on diagonal k, or -1 when the diagonal was not reached.
type pass struct {
	v      []int
	ks, ke int                 // diagonals trimmed off the low and high end after a path left the grid
	eq     func(x, y int) bool // whether the next step along a diagonal matches
}

func (p *pass) has(j int) bool { return j >= 0 && j < len(p.v) && p.v[j] != -1 }

// extend moves diagonal k to the furthest point reachable with d edits.
func (p *pass) extend(off, d, k, n, m int) (int, int) {
	i := off + k
	var x int
	if k == -d || (k != d && p.v[i-1] < p.v[i+1]) {
		x = p.v[i+1]
	} else {
		x = p.v[i-1] + 1
	}
	y := x - k
	for x < n && y < m && p.eq(x, y) {
		x++
		y++
	}
	p.v[i] = x
	return x, y
}

// inside reports whether (x, y) is on the grid; if not it trims the diagonal range.
func (p *pass) inside(x, y, n, m int) bool {
	switch {
	case x > n:
		p.ke += 2
		return false
	case y > m:
		p.ks += 2
		return false
	}
	return true
}

// middleSnake searches forward from the start and backward from the end, one
// edit at a time, until the two paths meet, and returns the meeting point
// (x in a, y in b). It reports false when the point does not split the
// problem into smaller ones.
func middleSnake(a, b []string) (int, int, bool) {
	n, m := len(a), len(b)
	maxD := (n + m + 1) / 2
	off := maxD
	newPass := func(eq func(x, y int) bool) *pass {
		v := make([]int, 2*maxD+2)
		for i := range v {
			v[i] = -1
		}
		v[off+1] = 0
		return &pass{v: v, eq: eq}
	}
	s := &snake{
		f:     newPass(func(x, y int) bool { return a[x] == b[y] }),
		r:     newPass(func(x, y int) bool { return a[n-x-1] == b[m-y-1] }),
		n:     n,
		m:     m,
		off:   off,
		delta: n - m,
	}
	s.front = s.delta%2 != 0 // the paths first meet in the forward pass when the lengths differ by an odd number
	for d := range maxD {
		if x, y, ok := s.forward(d); ok {
			return split(x, y, n, m)
		}
		if x, y, ok := s.reverse(d); ok {
			return split(x, y, n, m)
		}
	}
	return 0, 0, false
}

// snake holds both searches of one middleSnake call.
type snake struct {
	f, r             *pass
	n, m, off, delta int
	front            bool
}

// forward extends the forward paths by one edit and reports where they meet the backward ones.
func (s *snake) forward(d int) (int, int, bool) {
	for k := -d + s.f.ks; k <= d-s.f.ke; k += 2 {
		x, y := s.f.extend(s.off, d, k, s.n, s.m)
		if !s.f.inside(x, y, s.n, s.m) || !s.front {
			continue
		}
		if j := s.off + s.delta - k; s.r.has(j) && x >= s.n-s.r.v[j] {
			return x, y, true
		}
	}
	return 0, 0, false
}

// reverse extends the backward paths by one edit and reports where they meet the forward ones.
func (s *snake) reverse(d int) (int, int, bool) {
	for k := -d + s.r.ks; k <= d-s.r.ke; k += 2 {
		x, _ := s.r.extend(s.off, d, k, s.n, s.m)
		y := x - k
		if !s.r.inside(x, y, s.n, s.m) || s.front {
			continue
		}
		if j := s.off + s.delta - k; s.f.has(j) && s.f.v[j] >= s.n-x {
			return s.f.v[j], s.f.v[j] - (j - s.off), true
		}
	}
	return 0, 0, false
}

// split accepts a meeting point only when both halves are smaller than the whole.
func split(x, y, n, m int) (int, int, bool) {
	if x < 0 || y < 0 || x > n || y > m || (x == 0 && y == 0) || (x == n && y == m) {
		return 0, 0, false
	}
	return x, y, true
}
