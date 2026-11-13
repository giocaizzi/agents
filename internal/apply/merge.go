package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// planned is what one target would do to its file.
type planned struct {
	text   string     // new file text
	entry  *fileEntry // manifest entry to record; nil forgets the path
	same   bool       // the live document already equals the managed result
	skip   bool       // nothing managed and nothing to remove: leave the file alone
	remove bool       // delete the file
	paths  [][]string // key paths agents manages, for diffs
}

// plan works out the new file text and manifest entry of one target.
func plan(t Target, prev fileEntry, had bool, liveText string, exists bool) (planned, error) {
	if t.Format != Text {
		return merge(t, prev, had, liveText, exists)
	}
	s, ok := t.Managed.(string)
	if !ok {
		return planned{}, fmt.Errorf("text target needs a string, got %T", t.Managed)
	}
	entry := &fileEntry{Owner: t.Owner, Format: formatName(Text), Hash: hashText(s)}
	return planned{text: s, entry: entry, same: s == liveText}, nil
}

// merge applies the managed document onto the live one: it drops what the
// manifest says agents managed before and no longer does, then merges the
// managed keys. A target with nothing managed only removes.
func merge(t Target, prev fileEntry, had bool, liveText string, exists bool) (planned, error) {
	live, err := parseLive(t.Format, liveText)
	if err != nil {
		return planned{}, err
	}
	managed, err := normalize(t.Managed)
	if err != nil {
		return planned{}, err
	}
	if managed == nil {
		managed = NewObject()
	}
	mo, ok := managed.(*Object)
	if !ok {
		return planned{}, fmt.Errorf("managed document must be an object, got %T", t.Managed)
	}

	unions := map[string]bool{}
	for _, p := range t.Union {
		unions[pathID(p)] = true
	}
	entry := &fileEntry{Owner: t.Owner, Format: formatName(t.Format), Keys: [][]string{}, Union: map[string][]string{}}
	collect(mo, nil, unions, entry)
	if len(entry.Union) == 0 {
		entry.Union = nil
	}

	merged, _ := clone(live).(*Object)
	if !had {
		for _, p := range t.Legacy {
			deletePath(merged, p, false)
		}
	}
	dropStale(merged, prev, entry)
	p := planned{paths: managedPaths(prev, entry, t.Legacy)}
	empty := len(mo.Keys) == 0
	if !empty {
		mergeInto(merged, mo, nil, unions)
	}

	switch {
	case empty && !exists:
		p.skip = true
		return p, nil
	case empty:
		// Nothing is managed any more: forget the file, and delete it if
		// agents made it and nothing else is left in it.
		p.remove = prev.Created && len(merged.Keys) == 0
	default:
		entry.Created = prev.Created || !exists
		p.entry = entry
	}

	p.same = equal(merged, live)
	switch {
	case p.remove:
	case p.same && exists:
		p.text = liveText
	default:
		if p.text, err = renderOrPatch(t.Format, liveText, live, merged); err != nil {
			return planned{}, err
		}
	}
	return p, nil
}

// managedPaths lists every key path agents manages or managed in the file.
func managedPaths(prev fileEntry, next *fileEntry, legacy [][]string) [][]string {
	out := make([][]string, 0, len(prev.Keys)+len(next.Keys)+len(legacy)+len(prev.Union)+len(next.Union))
	out = append(out, prev.Keys...)
	out = append(out, next.Keys...)
	out = append(out, legacy...)
	for id := range prev.Union {
		out = append(out, pathOf(id))
	}
	for id := range next.Union {
		out = append(out, pathOf(id))
	}
	return out
}

// renderOrPatch returns the text of merged: the live text edited in place
// when there is one, so comments, key order and layout survive, else a fresh
// rendering.
func renderOrPatch(f Format, liveText string, live, merged *Object) (string, error) {
	if strings.TrimSpace(liveText) == "" {
		return render(f, merged)
	}
	var out string
	var err error
	if f == TOML {
		out, err = patchTOML(liveText, live, merged)
	} else {
		out, err = patchJSON(liveText, live, merged)
	}
	if err != nil {
		return "", fmt.Errorf("edit in place: %w", err)
	}
	// Never write a file whose document is not the merged one.
	back, err := parseLive(f, out)
	if err != nil {
		return "", fmt.Errorf("edit in place produced an unreadable file: %w", err)
	}
	if !equal(back, merged) {
		return "", errors.New("edit in place did not produce the merged document")
	}
	return out, nil
}

func parseLive(f Format, text string) (*Object, error) {
	if strings.TrimSpace(text) == "" {
		return NewObject(), nil
	}
	var doc any
	switch f {
	case JSON, JSONC:
		data := []byte(text)
		if f == JSONC {
			data = stripJSONC(data)
		}
		d, err := decodeJSON(data)
		if err != nil {
			return nil, fmt.Errorf("parse live file: %w", err)
		}
		doc = d
	case TOML:
		var m map[string]any
		if err := toml.Unmarshal([]byte(text), &m); err != nil {
			return nil, fmt.Errorf("parse live file: %w", err)
		}
		d, err := normalize(m)
		if err != nil {
			return nil, err
		}
		doc = d
	case Text:
		return nil, errors.New("text files have no document")
	}
	o, ok := doc.(*Object)
	if !ok {
		return nil, errors.New("live file is not an object at the top level")
	}
	return o, nil
}

func render(f Format, doc *Object) (string, error) {
	if f == TOML {
		out, err := toml.Marshal(toNative(doc))
		if err != nil {
			return "", fmt.Errorf("encode toml: %w", err)
		}
		return string(out), nil
	}
	out, err := encodeJSON(doc)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// toNative converts a document to plain Go values for go-toml.
func toNative(v any) any {
	switch t := v.(type) {
	case *Object:
		m := make(map[string]any, len(t.Keys))
		for _, k := range t.Keys {
			m[k] = toNative(t.Values[k])
		}
		return m
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = toNative(e)
		}
		return out
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		if f, err := t.Float64(); err == nil {
			return f
		}
		return string(t)
	default:
		return v
	}
}

// pathID is an unambiguous map key for a key path (keys may contain dots).
func pathID(p []string) string {
	raw, _ := json.Marshal(p) //nolint:errchkjson // strings cannot fail
	return string(raw)
}

func pathOf(id string) []string {
	var p []string
	_ = json.Unmarshal([]byte(id), &p)
	return p
}

// collect records the managed leaf paths and union elements of o.
func collect(o *Object, path []string, unions map[string]bool, entry *fileEntry) {
	for _, k := range o.Keys {
		p := append(append([]string(nil), path...), k)
		switch v := o.Values[k].(type) {
		case *Object:
			// An empty object is a container, never a leaf: recording it
			// would later delete whatever the user puts under it.
			collect(v, p, unions, entry)
		case []any:
			switch {
			case unions[pathID(p)]:
				elems := make([]string, len(v))
				for i, e := range v {
					elems[i] = canon(e)
				}
				entry.Union[pathID(p)] = elems
			case len(v) > 0:
				entry.Keys = append(entry.Keys, p)
			}
		default:
			entry.Keys = append(entry.Keys, p)
		}
	}
}

// dropStale deletes what the last run managed and the new managed document no longer has.
func dropStale(live *Object, prev fileEntry, next *fileEntry) {
	keep := map[string]bool{}
	for _, p := range next.Keys {
		keep[pathID(p)] = true
	}
	for _, p := range prev.Keys {
		// Manifests written before empty containers stopped being recorded
		// may list an object or an empty array: it holds the user's entries.
		if !keep[pathID(p)] && !isContainer(getPath(live, p)) {
			deletePath(live, p, true)
		}
	}
	for id, elems := range prev.Union {
		dropStaleUnion(live, pathOf(id), elems, next.Union[id])
	}
}

// dropStaleUnion removes the previously managed elems that are not in now from the array at path.
func dropStaleUnion(live *Object, path, elems, now []string) {
	keep := map[string]bool{}
	for _, c := range now {
		keep[c] = true
	}
	gone := map[string]bool{}
	for _, c := range elems {
		if !keep[c] {
			gone[c] = true
		}
	}
	arr, ok := getPath(live, path).([]any)
	if len(gone) == 0 || !ok {
		return
	}
	kept := make([]any, 0, len(arr))
	for _, e := range arr {
		if !gone[canon(e)] {
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 && now == nil {
		deletePath(live, path, true)
		return
	}
	setPath(live, path, kept)
}

// isContainer reports whether v is an object or an empty array.
func isContainer(v any) bool {
	switch t := v.(type) {
	case *Object:
		return true
	case []any:
		return len(t) == 0
	default:
		return false
	}
}

func getPath(o *Object, path []string) any {
	var cur any = o
	for _, k := range path {
		obj, ok := cur.(*Object)
		if !ok {
			return nil
		}
		cur = obj.Values[k]
	}
	return cur
}

func setPath(o *Object, path []string, v any) {
	for _, k := range path[:len(path)-1] {
		child, ok := o.Values[k].(*Object)
		if !ok {
			return
		}
		o = child
	}
	o.Set(path[len(path)-1], v)
}

// deletePath removes the key at path; with prune, parents left empty go too.
func deletePath(o *Object, path []string, prune bool) {
	switch len(path) {
	case 0:
		return
	case 1:
		o.Delete(path[0])
		return
	}
	child, ok := o.Values[path[0]].(*Object)
	if !ok {
		return
	}
	deletePath(child, path[1:], prune)
	if prune && len(child.Keys) == 0 {
		o.Delete(path[0])
	}
}

// mergeInto deep-merges src into dst: objects recurse, scalars and arrays replace, union paths append new elements.
func mergeInto(dst, src *Object, path []string, unions map[string]bool) {
	for _, k := range src.Keys {
		p := append(append([]string(nil), path...), k)
		sv := src.Values[k]
		switch v := sv.(type) {
		case *Object:
			if child, ok := dst.Values[k].(*Object); ok {
				mergeInto(child, v, p, unions)
			} else {
				dst.Set(k, clone(v))
			}
		case []any:
			existing, ok := dst.Values[k].([]any)
			if !ok || !unions[pathID(p)] {
				dst.Set(k, clone(v))
				continue
			}
			for _, e := range v {
				if !containsEqual(existing, e) {
					existing = append(existing, clone(e))
				}
			}
			dst.Set(k, existing)
		default:
			dst.Set(k, sv)
		}
	}
}

func containsEqual(list []any, v any) bool {
	for _, e := range list {
		if equal(e, v) {
			return true
		}
	}
	return false
}
