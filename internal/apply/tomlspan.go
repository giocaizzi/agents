package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2/unstable"
)

// tomlEntry is one key/value line or table header of a TOML file with its
// span, in whole lines.
type tomlEntry struct {
	kind     byte     // 'k' key/value, 't' table header, 'a' array-of-tables header
	path     []string // key/value: full key path; header: the table path
	section  int      // key/value: index of the header it sits under, -1 for the root
	start    int      // first byte of the first line
	end      int      // byte after the last line's newline
	valStart int      // key/value: span of the value
	valEnd   int
	inline   bool // key/value whose value is an inline table
}

// scanTOML lists the key/values and headers of src in file order.
func scanTOML(src string) ([]tomlEntry, error) {
	var p unstable.Parser
	p.Reset([]byte(src))
	var out []tomlEntry
	var table []string
	section := -1
	for p.NextExpression() {
		n := p.Expression()
		var keys []string
		var firstKey, lastKey unstable.Range
		it := n.Key()
		for it.Next() {
			k := it.Node()
			if len(keys) == 0 {
				firstKey = k.Raw
			}
			lastKey = k.Raw
			keys = append(keys, string(k.Data))
		}
		if n.Kind == unstable.KeyValue {
			e, err := keyValueEntry(src, n, keys, lastKey, table, section)
			if err != nil {
				return nil, err
			}
			out = append(out, e)
			continue
		}
		kind := byte('t')
		if n.Kind == unstable.ArrayTable {
			kind = 'a'
		}
		table = keys
		section = len(out)
		out = append(out, tomlEntry{
			kind: kind, path: keys, section: -1, start: lineStart(src, int(firstKey.Offset)),
			end: lineEnd(src, int(lastKey.Offset)) + 1,
		})
	}
	if err := p.Error(); err != nil {
		return nil, fmt.Errorf("scan toml: %w", err)
	}
	for i := range out {
		out[i].end = min(out[i].end, len(src))
	}
	return out, nil
}

func keyValueEntry(src string, n *unstable.Node, keys []string, lastKey unstable.Range, table []string, section int) (tomlEntry, error) {
	start := int(n.Raw.Offset)
	end := start + int(n.Raw.Length)
	vs := int(lastKey.Offset + lastKey.Length)
	for vs < len(src) && src[vs] != '=' {
		vs++
	}
	vs++
	for vs < len(src) && (src[vs] == ' ' || src[vs] == '\t') {
		vs++
	}
	if vs > end {
		return tomlEntry{}, errors.New("scan toml: cannot locate a value")
	}
	return tomlEntry{
		kind: 'k', path: append(slices.Clone(table), keys...), section: section,
		start: lineStart(src, start), end: lineEnd(src, end) + 1,
		valStart: vs, valEnd: end,
		inline: n.Value().Kind == unstable.InlineTable,
	}, nil
}

// tomlPatcher edits TOML text in place so that it holds the merged document.
type tomlPatcher struct {
	src     string
	entries []tomlEntry
	kv      map[string]*tomlEntry // exact key path -> key/value
	edits   []edit
	leaves  map[string][]string // parent path id -> lines to add
	order   []string            // parent path ids in first-seen order
	blocks  []string            // new tables for the end of the file
	err     error
}

// patchTOML returns src edited so that its document goes from live to merged.
func patchTOML(src string, live, merged *Object) (string, error) {
	entries, err := scanTOML(src)
	if err != nil {
		return "", err
	}
	p := &tomlPatcher{src: src, entries: entries, kv: map[string]*tomlEntry{}, leaves: map[string][]string{}}
	for i := range entries {
		if entries[i].kind == 'k' {
			p.kv[pathID(entries[i].path)] = &entries[i]
		}
	}
	p.object(nil, live, merged)
	p.flush()
	if p.err != nil {
		return "", p.err
	}
	return applyEdits(src, p.edits)
}

func (p *tomlPatcher) failf(format string, args ...any) {
	if p.err == nil {
		p.err = fmt.Errorf(format, args...)
	}
}

func (p *tomlPatcher) add(start, end int, text string) {
	p.edits = append(p.edits, edit{start, end, text})
}

func join(path []string, key string) []string {
	return append(slices.Clone(path), key)
}

func (p *tomlPatcher) object(path []string, live, merged *Object) {
	if kv := p.kv[pathID(path)]; kv != nil && kv.inline {
		// An inline table is one value: rewrite it whole.
		p.replace(kv, merged)
		return
	}
	for _, k := range live.Keys {
		if _, ok := merged.Values[k]; !ok {
			p.remove(join(path, k))
		}
	}
	for _, k := range merged.Keys {
		mv := merged.Values[k]
		lv, ok := live.Values[k]
		lo, lok := lv.(*Object)
		mo, mok := mv.(*Object)
		switch {
		case !ok:
			p.insert(path, k, mv)
		case equal(lv, mv):
		case lok && mok:
			p.object(join(path, k), lo, mo)
		default:
			kv := p.kv[pathID(join(path, k))]
			if kv == nil && lok {
				// A table written as sections becomes a scalar: drop the
				// sections, then write the scalar in the parent.
				p.remove(join(path, k))
				p.insert(path, k, mv)
				continue
			}
			if kv == nil {
				p.failf("toml: cannot rewrite %s in place", strings.Join(join(path, k), "."))
				return
			}
			p.replace(kv, mv)
		}
	}
}

func (p *tomlPatcher) replace(kv *tomlEntry, v any) {
	text, err := inlineTOML(v)
	if err != nil {
		p.failf("%v", err)
		return
	}
	p.add(kv.valStart, kv.valEnd, text)
}

// remove cuts every line at or under path.
func (p *tomlPatcher) remove(path []string) {
	found := false
	for _, e := range p.entries {
		if hasPrefixPath(e.path, path) {
			found = true
			p.add(e.start, e.end, "")
		}
	}
	if !found {
		p.failf("toml: cannot find %s to remove", strings.Join(path, "."))
	}
}

func (p *tomlPatcher) insert(parent []string, key string, v any) {
	if o, ok := v.(*Object); ok {
		p.blocks = append(p.blocks, tomlBlocks(join(parent, key), o, &p.err)...)
		return
	}
	text, err := inlineTOML(v)
	if err != nil {
		p.failf("%v", err)
		return
	}
	id := pathID(parent)
	if _, seen := p.leaves[id]; !seen {
		p.order = append(p.order, id)
	}
	p.leaves[id] = append(p.leaves[id], tomlKey(key)+" = "+text+"\n")
}

// flush places the new key/values under their tables and appends new tables.
func (p *tomlPatcher) flush() {
	for _, id := range p.order {
		parent := pathOf(id)
		lines := strings.Join(p.leaves[id], "")
		pos, ok := p.endOfSection(parent)
		if !ok {
			p.blocks = append(p.blocks, tomlHeader(parent)+lines)
			continue
		}
		if pos > 0 && p.src[pos-1] != '\n' {
			lines = "\n" + lines
		}
		if len(parent) == 0 && strings.HasPrefix(strings.TrimLeft(p.src[pos:], " \t"), "[") {
			lines += "\n"
		}
		p.add(pos, pos, lines)
	}
	if len(p.blocks) == 0 {
		return
	}
	var sb strings.Builder
	if p.src != "" && !strings.HasSuffix(p.src, "\n") {
		sb.WriteByte('\n')
	}
	for i, b := range p.blocks {
		if p.src != "" || i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(b)
	}
	p.add(len(p.src), len(p.src), sb.String())
}

// endOfSection returns where a new key/value of the table at parent goes:
// after the last key/value under its header, or for the root before the first
// header. It reports false when the table has no header of its own.
func (p *tomlPatcher) endOfSection(parent []string) (int, bool) {
	if len(parent) == 0 {
		pos := len(p.src)
		last := -1
		for i, e := range p.entries {
			if e.kind != 'k' {
				pos = e.start
				break
			}
			last = i
		}
		if last >= 0 {
			return p.entries[last].end, true
		}
		return pos, true
	}
	head := -1
	for i, e := range p.entries {
		if e.kind == 't' && slices.Equal(e.path, parent) {
			head = i
		}
	}
	if head < 0 {
		return 0, false
	}
	pos := p.entries[head].end
	for _, e := range p.entries {
		if e.kind == 'k' && e.section == head {
			pos = e.end
		}
	}
	return pos, true
}

// tomlBlocks renders o as a table at path followed by its sub-tables.
func tomlBlocks(path []string, o *Object, errp *error) []string {
	var leaves strings.Builder
	var subs []string
	for _, k := range o.Keys {
		if sub, ok := o.Values[k].(*Object); ok {
			subs = append(subs, tomlBlocks(join(path, k), sub, errp)...)
			continue
		}
		text, err := inlineTOML(o.Values[k])
		if err != nil && *errp == nil {
			*errp = err
		}
		leaves.WriteString(tomlKey(k) + " = " + text + "\n")
	}
	var out []string
	if leaves.Len() > 0 || len(subs) == 0 {
		out = append(out, tomlHeader(path)+leaves.String())
	}
	return append(out, subs...)
}

func tomlHeader(path []string) string {
	parts := make([]string, len(path))
	for i, k := range path {
		parts[i] = tomlKey(k)
	}
	return "[" + strings.Join(parts, ".") + "]\n"
}

func tomlKey(k string) string {
	if k != "" && strings.IndexFunc(k, notBareKeyRune) < 0 {
		return k
	}
	return tomlString(k)
}

// notBareKeyRune reports whether r cannot appear in an unquoted TOML key.
func notBareKeyRune(r rune) bool {
	bare := r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
	return !bare
}

func tomlString(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			sb.WriteString(`\"`)
		case r == '\\':
			sb.WriteString(`\\`)
		case r == '\n':
			sb.WriteString(`\n`)
		case r == '\t':
			sb.WriteString(`\t`)
		case r == '\r':
			sb.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&sb, `\u%04X`, r)
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// inlineTOML renders v as a TOML value on one line.
func inlineTOML(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return tomlString(t), nil
	case bool:
		return strconv.FormatBool(t), nil
	case json.Number:
		return string(t), nil
	case time.Time:
		return t.Format(time.RFC3339Nano), nil
	case *Object:
		parts := make([]string, len(t.Keys))
		for i, k := range t.Keys {
			s, err := inlineTOML(t.Values[k])
			if err != nil {
				return "", err
			}
			parts[i] = tomlKey(k) + " = " + s
		}
		if len(parts) == 0 {
			return "{}", nil
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			s, err := inlineTOML(e)
			if err != nil {
				return "", err
			}
			parts[i] = s
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case fmt.Stringer:
		return t.String(), nil
	default:
		return "", fmt.Errorf("toml cannot hold a %T", v)
	}
}

// maskTOML replaces the value of every key that managed rejects with a
// marker, so a diff of the result shows the structure without the values.
func maskTOML(src string, managed func(path []string) bool) (string, error) {
	entries, err := scanTOML(src)
	if err != nil {
		return "", err
	}
	var edits []edit
	for _, e := range entries {
		if e.kind == 'k' && !managed(e.path) {
			edits = append(edits, edit{e.valStart, e.valEnd, `"` + unmanagedMark + `"`})
		}
	}
	return applyEdits(src, edits)
}
