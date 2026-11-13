package apply

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// jnode is a JSON value with its byte span in the source text. Arrays and
// scalars are opaque: only objects expose their members.
type jnode struct {
	kind     byte // '{', '[' or 'v' (scalar)
	start    int
	end      int
	closeAt  int // objects: offset of the closing brace
	members  []jmember
	trailing bool // objects: the last member is followed by a comma
}

type jmember struct {
	key      string
	keyStart int
	val      jnode
	comma    int // offset of the comma after the value, -1 when none
}

type jparser struct {
	src string
	i   int
}

// parseJSONSpans parses JSON or JSONC text (comments and trailing commas
// allowed) into a span tree.
func parseJSONSpans(src string) (*jnode, error) {
	p := &jparser{src: src}
	p.skip()
	n, err := p.value()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.i != len(src) {
		return nil, errors.New("data after the top-level value")
	}
	return &n, nil
}

// skip moves past blanks and comments.
func (p *jparser) skip() {
	for p.i < len(p.src) {
		c := p.src[p.i]
		switch {
		case isSpace(c):
			p.i++
		case c == '/' && strings.HasPrefix(p.src[p.i:], "//"):
			p.i = lineEnd(p.src, p.i)
		case c == '/' && strings.HasPrefix(p.src[p.i:], "/*"):
			idx := strings.Index(p.src[p.i+2:], "*/")
			if idx < 0 {
				p.i = len(p.src)
				return
			}
			p.i += idx + 4
		default:
			return
		}
	}
}

// lineEnd returns the offset of the newline ending the line at or after from, or len(src).
func lineEnd(src string, from int) int {
	if i := strings.IndexByte(src[from:], '\n'); i >= 0 {
		return from + i
	}
	return len(src)
}

func (p *jparser) value() (jnode, error) {
	if p.i >= len(p.src) {
		return jnode{}, errors.New("unexpected end of input")
	}
	switch p.src[p.i] {
	case '{':
		return p.object()
	case '[':
		return p.array()
	case '"':
		start := p.i
		end, err := p.str()
		return jnode{kind: 'v', start: start, end: end}, err
	default:
		start := p.i
		for p.i < len(p.src) && !isSpace(p.src[p.i]) && !strings.ContainsRune(",}]/", rune(p.src[p.i])) {
			p.i++
		}
		if p.i == start {
			return jnode{}, fmt.Errorf("unexpected %q at offset %d", p.src[p.i], p.i)
		}
		return jnode{kind: 'v', start: start, end: p.i}, nil
	}
}

// str scans the string literal at p.i and returns the offset after it.
func (p *jparser) str() (int, error) {
	for p.i++; p.i < len(p.src); p.i++ {
		switch p.src[p.i] {
		case '\\':
			p.i++
		case '"':
			p.i++
			return p.i, nil
		}
	}
	return 0, errors.New("unterminated string")
}

func (p *jparser) object() (jnode, error) {
	n := jnode{kind: '{', start: p.i}
	p.i++
	needClose := false
	for {
		p.skip()
		if p.i >= len(p.src) {
			return n, errors.New("unterminated object")
		}
		if p.src[p.i] == '}' {
			n.closeAt = p.i
			p.i++
			n.end = p.i
			n.trailing = len(n.members) > 0 && n.members[len(n.members)-1].comma >= 0
			return n, nil
		}
		if needClose || p.src[p.i] != '"' {
			return n, fmt.Errorf("unexpected %q at offset %d", p.src[p.i], p.i)
		}
		m, err := p.member()
		if err != nil {
			return n, err
		}
		needClose = m.comma < 0
		n.members = append(n.members, m)
	}
}

// member parses `"key": value` and the comma after it, if any.
func (p *jparser) member() (jmember, error) {
	keyStart := p.i
	keyEnd, err := p.str()
	if err != nil {
		return jmember{}, err
	}
	var key string
	if uerr := json.Unmarshal([]byte(p.src[keyStart:keyEnd]), &key); uerr != nil {
		return jmember{}, fmt.Errorf("decode key: %w", uerr)
	}
	p.skip()
	if p.i >= len(p.src) || p.src[p.i] != ':' {
		return jmember{}, fmt.Errorf("expected ':' at offset %d", p.i)
	}
	p.i++
	p.skip()
	v, err := p.value()
	if err != nil {
		return jmember{}, err
	}
	m := jmember{key: key, keyStart: keyStart, val: v, comma: -1}
	p.skip()
	if p.i < len(p.src) && p.src[p.i] == ',' {
		m.comma = p.i
		p.i++
	}
	return m, nil
}

func (p *jparser) array() (jnode, error) {
	n := jnode{kind: '[', start: p.i}
	p.i++
	needClose := false
	for {
		p.skip()
		if p.i >= len(p.src) {
			return n, errors.New("unterminated array")
		}
		if p.src[p.i] == ']' {
			p.i++
			n.end = p.i
			return n, nil
		}
		if needClose {
			return n, fmt.Errorf("unexpected %q at offset %d", p.src[p.i], p.i)
		}
		if _, err := p.value(); err != nil {
			return n, err
		}
		p.skip()
		needClose = true
		if p.i < len(p.src) && p.src[p.i] == ',' {
			p.i++
			needClose = false
		}
	}
}

// jsonPatcher edits JSON text in place so that it holds the merged document,
// leaving comments, key order and layout alone wherever nothing changes.
type jsonPatcher struct {
	src   string
	unit  string // one indent level
	edits []edit
	err   error
}

// patchJSON returns src edited so that its document goes from live to merged.
func patchJSON(src string, live, merged *Object) (string, error) {
	root, err := parseJSONSpans(src)
	if err != nil {
		return "", fmt.Errorf("scan json: %w", err)
	}
	if root.kind != '{' {
		return "", errors.New("live file is not an object at the top level")
	}
	p := &jsonPatcher{src: src, unit: "  "}
	if len(root.members) > 0 {
		k := root.members[0].keyStart
		if ind := lineIndent(src, k); ind != "" && strings.TrimSpace(src[lineStart(src, k):k]) == "" {
			p.unit = ind
		}
	}
	p.object(root, live, merged, "")
	if p.err != nil {
		return "", p.err
	}
	return applyEdits(src, p.edits)
}

func (p *jsonPatcher) add(start, end int, text string) {
	p.edits = append(p.edits, edit{start, end, text})
}

// value renders v for a member whose line is indented by indent.
func (p *jsonPatcher) value(v any, indent string) string {
	var buf bytes.Buffer
	if err := writeJSON(&buf, v, 0, p.unit); err != nil {
		p.err = err
	}
	return strings.ReplaceAll(buf.String(), "\n", "\n"+indent)
}

func compactJSON(v any) string {
	switch t := v.(type) {
	case *Object:
		parts := make([]string, len(t.Keys))
		for i, k := range t.Keys {
			parts[i] = plainString(k) + ": " + compactJSON(t.Values[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = compactJSON(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return plainString(v)
	}
}

func (p *jsonPatcher) childIndent(n *jnode, indent string) string {
	if len(n.members) > 0 {
		k := n.members[0].keyStart
		if strings.TrimSpace(p.src[lineStart(p.src, k):k]) == "" {
			return lineIndent(p.src, k)
		}
	}
	return indent + p.unit
}

// memberRange is the text to cut to delete m: the member, its comma and,
// when it fills its lines, the lines themselves.
func (p *jsonPatcher) memberRange(m *jmember) (int, int) {
	start, end := m.keyStart, m.val.end
	if m.comma >= 0 {
		end = m.comma + 1
	}
	j := end
	for j < len(p.src) && (p.src[j] == ' ' || p.src[j] == '\t' || p.src[j] == '\r') {
		j++
	}
	ls := lineStart(p.src, start)
	alone := strings.TrimSpace(p.src[ls:start]) == ""
	switch {
	case alone && j >= len(p.src):
		return ls, j
	case alone && p.src[j] == '\n':
		return ls, j + 1
	default:
		if j < len(p.src) && p.src[j] == '}' {
			for start > 0 && (p.src[start-1] == ' ' || p.src[start-1] == '\t') {
				start--
			}
		}
		return start, j
	}
}

// eol returns where text can be appended after from without running into
// other tokens: before the newline (and any line comment) when only those
// follow on the line, else from itself.
func (p *jsonPatcher) eol(from int) int {
	j := from
	for j < len(p.src) && (p.src[j] == ' ' || p.src[j] == '\t') {
		j++
	}
	if strings.HasPrefix(p.src[j:], "//") {
		j = lineEnd(p.src, j)
	}
	if j < len(p.src) && p.src[j] == '\r' {
		j++
	}
	if j >= len(p.src) || p.src[j] != '\n' {
		return from
	}
	if j > from && p.src[j-1] == '\r' {
		return j - 1
	}
	return j
}

// object reconciles the members of n (the text of live) with merged.
func (p *jsonPatcher) object(n *jnode, live, merged *Object, indent string) {
	multiline := len(n.members) == 0 || strings.Contains(p.src[n.start:n.closeAt], "\n")
	ins := p.update(n, live, merged, multiline)
	var deleted []int
	last := -1
	for i := range n.members {
		if _, keep := merged.Values[n.members[i].key]; keep {
			last = i
		} else {
			deleted = append(deleted, i)
		}
	}
	if len(deleted) == 0 && len(ins) == 0 {
		return
	}
	child := p.childIndent(n, indent)
	items := make([]string, len(ins))
	for i, k := range ins {
		if multiline {
			items[i] = plainString(k) + ": " + p.value(merged.Values[k], child)
		} else {
			items[i] = plainString(k) + ": " + compactJSON(merged.Values[k])
		}
	}
	if last < 0 {
		p.replaceInterior(n, items, child, indent)
		return
	}
	for _, i := range deleted {
		s, e := p.memberRange(&n.members[i])
		p.add(s, e, "")
	}
	s := &n.members[last]
	if len(ins) == 0 {
		// Dropping the tail leaves the last survivor's comma dangling.
		if last < len(n.members)-1 && s.comma >= 0 && !n.trailing {
			p.add(s.comma, s.comma+1, "")
		}
		return
	}
	p.appendItems(n, s, items, child, multiline)
}

// update rewrites the members present in both documents whose value changed
// and returns the keys only merged has.
func (p *jsonPatcher) update(n *jnode, live, merged *Object, multiline bool) []string {
	idx := make(map[string]*jmember, len(n.members))
	for i := range n.members {
		idx[n.members[i].key] = &n.members[i]
	}
	var ins []string
	for _, k := range merged.Keys {
		m, ok := idx[k]
		if !ok {
			ins = append(ins, k)
			continue
		}
		lv, mv := live.Values[k], merged.Values[k]
		if equal(lv, mv) {
			continue
		}
		mi := lineIndent(p.src, m.keyStart)
		lo, lok := lv.(*Object)
		mo, mok := mv.(*Object)
		if lok && mok && m.val.kind == '{' {
			p.object(&m.val, lo, mo, mi)
			continue
		}
		text := compactJSON(mv)
		_, isObj := mv.(*Object)
		if strings.Contains(p.src[m.val.start:m.val.end], "\n") || (isObj && multiline) {
			text = p.value(mv, mi)
		}
		p.add(m.val.start, m.val.end, text)
	}
	return ins
}

// appendItems adds items after s, the last member that stays.
func (p *jsonPatcher) appendItems(n *jnode, s *jmember, items []string, child string, multiline bool) {
	after := s.val.end
	if s.comma >= 0 {
		after = s.comma + 1
	} else {
		p.add(after, after, ",")
	}
	tail := ""
	if n.trailing {
		tail = ","
	}
	if multiline {
		pos := p.eol(after)
		p.add(pos, pos, "\n"+child+strings.Join(items, ",\n"+child)+tail)
		return
	}
	p.add(after, after, " "+strings.Join(items, ", ")+tail)
}

// replaceInterior sets the members of an object with no survivors to items.
func (p *jsonPatcher) replaceInterior(n *jnode, items []string, child, indent string) {
	from, to := n.start+1, n.closeAt
	text := ""
	if len(items) > 0 {
		text = "\n" + child + strings.Join(items, ",\n"+child) + "\n" + indent
	}
	if len(n.members) == 0 && strings.TrimSpace(p.src[from:to]) != "" {
		// Only comments inside: keep them and add before the brace.
		p.add(to, to, text)
		return
	}
	p.add(from, to, text)
}

// maskJSON replaces the value of every key that managed rejects with a
// marker, so a diff of the result shows the structure without the values.
func maskJSON(src string, managed func(path []string) bool) (string, error) {
	root, err := parseJSONSpans(src)
	if err != nil {
		return "", fmt.Errorf("scan json: %w", err)
	}
	var edits []edit
	var walk func(n *jnode, path []string)
	walk = func(n *jnode, path []string) {
		for i := range n.members {
			m := &n.members[i]
			p := append(append([]string(nil), path...), m.key)
			switch {
			case m.val.kind == '{':
				walk(&m.val, p)
			case !managed(p):
				edits = append(edits, edit{m.val.start, m.val.end, `"` + unmanagedMark + `"`})
			}
		}
	}
	walk(root, nil)
	return applyEdits(src, edits)
}
