package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// errChanged reports a file that changed between being read and being replaced.
var errChanged = errors.New("changed while agents was running; run apply again")

// Engine applies Targets and remembers what it manages in a manifest.
type Engine struct {
	StatePath string // manifest file, e.g. $XDG_STATE_HOME/agents/apply.json
	DryRun    bool   // compute statuses and diffs, write nothing
	// Diff asks for Result.Diff on a real run; a dry run always builds diffs.
	// Without either, no diff is computed.
	Diff bool
	// Partial says the targets are a subset of what agents manages: manifest
	// paths missing from them are left alone. Otherwise they are retired.
	Partial bool
}

// Run applies every target and returns one Result per target, in order. A
// per-target failure is reported in Result.Err; the returned error is for
// failures of the manifest itself.
//
// Unless Partial is set, every path the manifest lists that is not among the
// targets is retired after them: the keys agents managed there are removed,
// a file agents created that is left empty is deleted, and a Text file is
// deleted only while it still holds what agents wrote (else it is reported as
// drift and left). Those results follow the targets' results.
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
	listed := map[string]bool{}
	for _, t := range targets {
		listed[t.Path] = true
		prev, had := old.Files[t.Path]
		res, entry, forget := e.apply(t, prev, had)
		results = append(results, res)
		next.record(t.Path, entry, forget)
	}
	if !e.Partial {
		paths := make([]string, 0, len(old.Files))
		for path := range old.Files {
			if !listed[path] {
				paths = append(paths, path)
			}
		}
		slices.Sort(paths)
		for _, path := range paths {
			res, entry, forget := e.retire(path, old.Files[path])
			if res != nil {
				results = append(results, *res)
			}
			next.record(path, entry, forget)
		}
	}
	if e.StatePath != "" && !e.DryRun && !next.equal(old) {
		if err := next.save(e.StatePath); err != nil {
			return results, fmt.Errorf("save manifest: %w", err)
		}
	}
	return results, nil
}

func (e Engine) wantDiff() bool { return e.DryRun || e.Diff }

// apply handles one target. The entry is what the manifest should record for
// the path; forget asks it to drop the path instead. With neither, the
// manifest keeps what it had.
func (e Engine) apply(t Target, prev fileEntry, had bool) (res Result, entry *fileEntry, forget bool) {
	res = Result{Target: t}
	if !filepath.IsAbs(t.Path) {
		res.Err = fmt.Errorf("%s: %s: path is not absolute", t.Owner, t.Path)
		return res, nil, false
	}
	if info, err := os.Lstat(t.Path); err == nil && info.Mode()&fs.ModeSymlink != 0 && !t.CheckOnly {
		res.Status = StatusRefused
		res.Err = fmt.Errorf("%s: %s is a symlink: agents does not write through symlinks", t.Owner, t.Path)
		return res, nil, false
	}

	raw, err := os.ReadFile(t.Path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		res.Err = fmt.Errorf("%s: %s: read: %w", t.Owner, t.Path, err)
		return res, nil, false
	}
	liveText := string(raw)

	p, err := plan(t, prev, had, liveText, exists)
	if err != nil {
		res.Err = fmt.Errorf("%s: %s: %w", t.Owner, t.Path, err)
		return res, nil, false
	}
	if p.skip {
		res.Status = StatusUnchanged
		return res, nil, true
	}
	same := p.same && exists && !p.remove

	switch {
	case same:
		res.Status = StatusUnchanged
	case p.remove:
		res.Status = StatusDelete
	case t.CheckOnly:
		res.Status = StatusDrift
	case exists:
		res.Status = StatusUpdate
	default:
		res.Status = StatusCreate
	}
	if !same && e.wantDiff() {
		res.Diff = buildDiff(t, p.paths, liveText, p.text)
	}
	if t.CheckOnly {
		return res, nil, false
	}
	if !e.DryRun && !same {
		var werr error
		if p.remove {
			werr = removeFile(t.Path, liveText)
		} else {
			werr = writeFile(t, []byte(p.text), liveText, exists)
		}
		if werr != nil {
			res.Err = fmt.Errorf("%s: %w", t.Owner, werr)
			res.Status = ""
			return res, nil, false
		}
	}
	return res, p.entry, p.entry == nil
}

// retire handles a path the manifest lists that no target produces any more.
// A nil result means there was nothing to report.
func (e Engine) retire(path string, ent fileEntry) (*Result, *fileEntry, bool) {
	f, ok := parseFormat(ent.Format)
	if !ok {
		return nil, nil, false // a manifest this version cannot read: leave it
	}
	if f == Text {
		return e.retireText(path, ent)
	}
	t := Target{Owner: ent.Owner, Path: path, Format: f, Managed: map[string]any{}}
	res, entry, forget := e.apply(t, ent, true)
	if res.Err == nil && res.Status == StatusUnchanged {
		return nil, entry, forget
	}
	return &res, entry, forget
}

// retireText deletes a Text file agents wrote if nobody edited it since.
func (e Engine) retireText(path string, ent fileEntry) (*Result, *fileEntry, bool) {
	res := Result{Target: Target{Owner: ent.Owner, Path: path, Format: Text}}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, true
	}
	if err != nil {
		res.Err = fmt.Errorf("%s: %s: %w", ent.Owner, path, err)
		return &res, nil, false
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		res.Status = StatusRefused
		res.Err = fmt.Errorf("%s: %s is a symlink: agents does not write through symlinks", ent.Owner, path)
		return &res, nil, false
	}
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		res.Err = fmt.Errorf("%s: %s: read: %w", ent.Owner, path, err)
		return &res, nil, false
	}
	if hashText(string(raw)) != ent.Hash {
		// Edited since agents wrote it: it is the user's now.
		res.Status = StatusDrift
		return &res, nil, true
	}
	res.Status = StatusDelete
	if e.wantDiff() {
		res.Diff = buildDiff(res.Target, nil, string(raw), "")
	}
	if !e.DryRun {
		if err := removeFile(path, string(raw)); err != nil {
			res.Err = fmt.Errorf("%s: %w", ent.Owner, err)
			res.Status = ""
			return &res, nil, false
		}
	}
	return &res, nil, true
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// unchangedSince returns the check writeAtomic runs just before it replaces
// path: the file must still hold what was read (or still be absent).
func unchangedSince(path, text string, existed bool) func() error {
	return func() error {
		cur, err := os.ReadFile(filepath.Clean(path))
		switch {
		case existed && (err != nil || string(cur) != text):
			return errChanged
		case !existed && !errors.Is(err, fs.ErrNotExist):
			return errChanged
		}
		return nil
	}
}

func writeFile(t Target, data []byte, liveText string, existed bool) error {
	mode := t.Mode
	if mode == 0 {
		mode = 0o600
	}
	if info, err := os.Stat(t.Path); err == nil {
		mode = info.Mode().Perm()
		if len(t.Sensitive) > 0 && mode != 0o600 {
			// Tighten before writing so secrets never land in a wider file.
			mode = 0o600
			if err := os.Chmod(t.Path, mode); err != nil {
				return fmt.Errorf("chmod %s: %w", t.Path, err)
			}
		}
	} else if len(t.Sensitive) > 0 {
		mode = 0o600
	}
	if err := os.MkdirAll(filepath.Dir(t.Path), 0o700); err != nil {
		return fmt.Errorf("create dir for %s: %w", t.Path, err)
	}
	if err := writeAtomic(t.Path, data, mode, unchangedSince(t.Path, liveText, existed)); err != nil {
		return err
	}
	return nil
}

func removeFile(path, liveText string) error {
	if err := unchangedSince(path, liveText, true)(); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
