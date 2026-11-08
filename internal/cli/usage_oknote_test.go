package cli

import (
	"testing"

	"github.com/giocaizzi/agents/internal/provider"
	usg "github.com/giocaizzi/agents/internal/usage"
)

// An OK account keeps its note, and the row shows it; a non-OK account's note
// is left to the fetch result.
func TestOKNoteReachesTheRow(t *testing.T) {
	t.Parallel()
	f := testFetcher(t, nil)
	acct := oauthAccount(provider.Anthropic)
	acct.Note = "account profile too large (.claude.json): identity unknown"
	snap := f.base(acct)
	if snap.Note != acct.Note {
		t.Errorf("base note = %q, want %q", snap.Note, acct.Note)
	}
	if got := warning(snap); got != acct.Note {
		t.Errorf("warning = %q, want the note", got)
	}
	acct.Status = usg.StatusExpired
	if got := f.base(acct).Note; got != "" {
		t.Errorf("base note of a non-OK account = %q, want empty", got)
	}
	if got := warning(usg.Snapshot{Status: usg.StatusOK}); got != "" {
		t.Errorf("warning of a clean OK row = %q, want empty", got)
	}
}
