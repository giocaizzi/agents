package provider

import (
	"cmp"
	"slices"
	"strings"
)

// Discover returns every account found, merged by (provider, key) with the
// harnesses that use it, sorted by provider then label then key.
func Discover(st Store) []Account {
	readers := []func(Store) []Account{readClaude, readCodex, readCopilot, readAntigravity, readOpencode, readPi}
	all := make([]Account, 0, len(readers))
	for _, read := range readers {
		all = append(all, read(st)...)
	}
	return Merge(all)
}

// Merge merges accounts by (provider, key) with the harnesses that use them,
// sorted by provider then label then key. It is idempotent, so accounts that
// gained a proven identity after Discover can be merged again.
func Merge(all []Account) []Account {
	type groupKey struct{ provider, key string }
	index := map[groupKey]int{}
	var out []Account
	for _, a := range all {
		if a.Key == "" {
			out = append(out, a)
			continue
		}
		k := groupKey{a.Provider, a.Key}
		i, ok := index[k]
		if !ok {
			index[k] = len(out)
			out = append(out, a)
			continue
		}
		out[i] = merge(out[i], a)
	}

	for i := range out {
		slices.Sort(out[i].Harnesses)
		out[i].Harnesses = slices.Compact(out[i].Harnesses)
	}
	slices.SortStableFunc(out, func(a, b Account) int {
		return cmp.Or(
			cmp.Compare(a.Provider, b.Provider),
			cmp.Compare(a.Label, b.Label),
			cmp.Compare(a.Key, b.Key),
			strings.Compare(strings.Join(a.Harnesses, ","), strings.Join(b.Harnesses, ",")),
		)
	})
	return out
}

// newer reports whether b is more recent than a: its token expires later.
func newer(a, b Account) bool {
	return b.Expires.After(a.Expires)
}

// merge combines two accounts of the same (provider, key). The newer one
// provides the credential, expiry, plan and status; harnesses are united, and
// a non-empty label or login is kept. The result is inactive only when both
// are; otherwise the inactive note is dropped from the winner's note.
func merge(a, b Account) Account {
	win, lose := a, b
	switch {
	case a.LookupFailed != b.LookupFailed:
		// A credential whose live lookup failed never wins over one without.
		if a.LookupFailed {
			win, lose = b, a
		}
	case newer(a, b):
		win, lose = b, a
	}
	win.ID = min(a.ID, b.ID)
	win.Harnesses = append(slices.Clone(a.Harnesses), b.Harnesses...)
	if win.Label == "" {
		win.Label = lose.Label
	}
	if win.Login == "" {
		win.Login = lose.Login
	}
	if win.CodexHome == "" {
		win.CodexHome = lose.CodexHome
	}
	win.IdentityKnown = a.IdentityKnown || b.IdentityKnown
	win.Fingerprints = unionFingerprints(a.Fingerprints, b.Fingerprints)
	win.Inactive = a.Inactive && b.Inactive
	win.LookupFailed = a.LookupFailed && b.LookupFailed
	if !win.Inactive {
		win.Note = strings.TrimSpace(strings.TrimSuffix(win.Note, inactiveNote))
	}
	return win
}
