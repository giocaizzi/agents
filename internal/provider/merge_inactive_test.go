package provider

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestMergeInactive(t *testing.T) {
	t.Parallel()
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	acct := func(id string, inactive bool, exp time.Time) Account {
		a := Account{Provider: OpenAI, Key: "openai:acct:user", ID: id, Expires: exp, Inactive: inactive, Note: "n"}
		if inactive {
			a.Note = "n " + inactiveNote
		}
		return a
	}
	tests := []struct {
		name         string
		in           []Account
		wantNote     string
		wantInactive bool
	}{
		{"inactive wins over active", []Account{acct("a", true, base.Add(time.Hour)), acct("b", false, base)}, "n", false},
		{"active wins over inactive", []Account{acct("a", false, base.Add(time.Hour)), acct("b", true, base)}, "n", false},
		{"inactive first, active newer", []Account{acct("a", true, base), acct("b", false, base.Add(time.Hour))}, "n", false},
		{"active first, inactive newer", []Account{acct("a", false, base), acct("b", true, base.Add(time.Hour))}, "n", false},
		{"both inactive", []Account{acct("a", true, base), acct("b", true, base.Add(time.Hour))}, "n " + inactiveNote, true},
		{"lone inactive", []Account{acct("a", true, base)}, "n " + inactiveNote, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Merge(tt.in)
			if len(got) != 1 {
				t.Fatalf("rows = %d, want 1", len(got))
			}
			if diff := cmp.Diff([]any{tt.wantNote, tt.wantInactive}, []any{got[0].Note, got[0].Inactive}); diff != "" {
				t.Errorf("(note, inactive) mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestIdentifyAccountID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		cred      Credential
		wantToken string
		wantID    string
	}{
		{"no account id gets one", NewCredential("tok", ""), "tok", "acct-1"},
		{"existing account id kept", NewCredential("tok", "acct-old"), "tok", "acct-old"},
		{"empty credential stays empty", Credential{}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Identify(Account{Provider: OpenAI, Cred: tt.cred}, Identity{Key: "openai:acct-1:user-1", AccountID: "acct-1"})
			if got.Cred.Token() != tt.wantToken || got.Cred.AccountID() != tt.wantID {
				t.Errorf("cred = (%q, %q), want (%q, %q)", got.Cred.Token(), got.Cred.AccountID(), tt.wantToken, tt.wantID)
			}
		})
	}
}
