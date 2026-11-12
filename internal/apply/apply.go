package apply

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Format is the on-disk syntax of a Target.
type Format int

// Supported formats.
const (
	JSON Format = iota
	JSONC
	TOML
	Text
)

// Target is one native file and the keys agents manages in it.
type Target struct {
	Owner     string      // harness name, for messages only
	Path      string      // absolute
	Format    Format      // syntax of the file
	Managed   any         // JSON/JSONC/TOML: map[string]any or *Object (ordered); Text: string (whole file content)
	Union     [][]string  // key paths whose arrays merge as a union (live elements kept, managed ones appended when new)
	Remove    [][]string  // key paths always deleted from the live document before merging
	Mode      fs.FileMode // mode for a new file; 0 means 0o600
	CheckOnly bool        // never written: compared only (drift)
}

// Status is the outcome for one Target.
type Status string

// Possible statuses.
const (
	StatusCreate    Status = "create"
	StatusUpdate    Status = "update"
	StatusUnchanged Status = "unchanged"
	StatusDrift     Status = "drift"   // CheckOnly target differs
	StatusRefused   Status = "refused" // symlink target that is not CheckOnly
)

// Result reports what happened to one Target.
type Result struct {
	Target Target
	Status Status
	Diff   string // unified diff of the change, empty when unchanged
	Err    error
}

// Engine applies Targets and remembers what it manages in a manifest.
type Engine struct {
	StatePath string // manifest file, e.g. $XDG_STATE_HOME/agents/apply.json
	DryRun    bool   // compute statuses and diffs, write nothing
}

// Run applies every target and returns one Result per target, in order. A
// per-target failure is reported in Result.Err; the returned error is for
// failures of the manifest itself.
func (e Engine) Run(targets []Target) ([]Result, error) {
	old := newManifest()
	if e.StatePath != "" {
		var err error
		if old, err = loadManifest(e.StatePath); err != nil {
			return nil, err
		}
	}
	next := old.clone()
	results := make([]Result, 0, len(targets))
	for _, t := range targets {
		res, entry := e.apply(t, old.Files[t.Path])
		results = append(results, res)
		if entry != nil {
			next.Files[t.Path] = *entry
		}
	}
	if e.StatePath != "" && !e.DryRun && !next.equal(old) {
		if err := next.save(e.StatePath); err != nil {
			return results, fmt.Errorf("save manifest: %w", err)
		}
	}
	return results, nil
}

// apply handles one target; the entry is non-nil when the manifest should record the file.
func (e Engine) apply(t Target, prev fileEntry) (Result, *fileEntry) {
	res := Result{Target: t}
	if !filepath.IsAbs(t.Path) {
		res.Err = fmt.Errorf("%s: %s: path is not absolute", t.Owner, t.Path)
		return res, nil
	}
	if info, err := os.Lstat(t.Path); err == nil && info.Mode()&fs.ModeSymlink != 0 && !t.CheckOnly {
		res.Status = StatusRefused
		res.Err = fmt.Errorf("%s: %s is a symlink: agents does not write through symlinks", t.Owner, t.Path)
		return res, nil
	}

	raw, err := os.ReadFile(t.Path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		res.Err = fmt.Errorf("%s: %s: read: %w", t.Owner, t.Path, err)
		return res, nil
	}
	liveText := string(raw)

	newText, entry, same, err := plan(t, prev, liveText)
	if err != nil {
		res.Err = fmt.Errorf("%s: %s: %w", t.Owner, t.Path, err)
		return res, nil
	}
	same = same && exists

	switch {
	case same:
		res.Status = StatusUnchanged
	case t.CheckOnly:
		res.Status = StatusDrift
	case exists:
		res.Status = StatusUpdate
	default:
		res.Status = StatusCreate
	}
	if !same {
		res.Diff = unifiedDiff(t.Path, liveText, newText)
	}
	if t.CheckOnly {
		return res, nil
	}
	if !e.DryRun && !same {
		if err := writeFile(t, []byte(newText)); err != nil {
			res.Err = fmt.Errorf("%s: %w", t.Owner, err)
			res.Status = ""
			return res, nil
		}
	}
	return res, entry
}

// plan returns the new file text, the manifest entry and whether the live
// document already equals the managed result.
func plan(t Target, prev fileEntry, liveText string) (string, *fileEntry, bool, error) {
	if t.Format != Text {
		return merge(t, prev, liveText)
	}
	s, ok := t.Managed.(string)
	if !ok {
		return "", nil, false, fmt.Errorf("text target needs a string, got %T", t.Managed)
	}
	return s, nil, s == liveText, nil
}

func writeFile(t Target, data []byte) error {
	mode := t.Mode
	if mode == 0 {
		mode = 0o600
	}
	if info, err := os.Stat(t.Path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(t.Path), 0o700); err != nil {
		return fmt.Errorf("create dir for %s: %w", t.Path, err)
	}
	return writeAtomic(t.Path, data, mode)
}

// merge returns the rendering, the manifest entry and
// whether the merged document equals the live one.
func merge(t Target, prev fileEntry, liveText string) (string, *fileEntry, bool, error) {
	live, err := parseLive(t.Format, liveText)
	if err != nil {
		return "", nil, false, err
	}
	managed, err := normalize(t.Managed)
	if err != nil {
		return "", nil, false, err
	}
	if managed == nil {
		managed = NewObject()
	}
	mo, ok := managed.(*Object)
	if !ok {
		return "", nil, false, fmt.Errorf("managed document must be an object, got %T", t.Managed)
	}

	unions := map[string]bool{}
	for _, p := range t.Union {
		unions[pathID(p)] = true
	}
	entry := &fileEntry{Keys: [][]string{}, Union: map[string][]string{}}
	collect(mo, nil, unions, entry)
	if len(entry.Union) == 0 {
		entry.Union = nil
	}

	merged, _ := clone(live).(*Object)
	for _, p := range t.Remove {
		deletePath(merged, p, false)
	}
	dropStale(merged, prev, entry)
	mergeInto(merged, mo, nil, unions)

	text, err := render(t.Format, merged)
	if err != nil {
		return "", nil, false, err
	}
	return text, entry, equal(merged, live), nil
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
			if len(v.Keys) == 0 {
				entry.Keys = append(entry.Keys, p)
			} else {
				collect(v, p, unions, entry)
			}
		case []any:
			if unions[pathID(p)] {
				elems := make([]string, len(v))
				for i, e := range v {
					elems[i] = canon(e)
				}
				entry.Union[pathID(p)] = elems
			} else {
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
		if !keep[pathID(p)] {
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
