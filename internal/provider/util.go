package provider

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/giocaizzi/agents/internal/usage"
)

// Where an account was read (Account.Source).
const (
	srcFile     = "file"
	srcEnv      = "env"
	srcKeychain = "keychain"
	srcSQLite   = "sqlite"
)

// Harness names used more than once.
const (
	harnessClaude      = "claude"
	harnessOpencode    = "opencode"
	harnessAntigravity = "antigravity"
)

// noteUnreadable is the Note of an account whose store could not be read. It
// never carries file content.
const noteUnreadable = "credentials unreadable"

// openaiAuthClaim is the JWT claim OpenAI uses for ChatGPT account facts.
const openaiAuthClaim = "https://api.openai.com/auth"

// openaiProfileClaim is the JWT claim OpenAI uses for the ChatGPT user profile.
const openaiProfileClaim = "https://api.openai.com/profile"

// unknownPlan is the plan of an account whose credential does not say.
func unknownPlan() usage.Plan {
	return usage.Plan{Kind: usage.KindUnknown, Billing: usage.BillingUnknown, Payer: usage.PayerUnknown}
}

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

// errTooLargeFile is readJSON's fixed error for a file over the cap.
var errTooLargeFile = fmt.Errorf("unreadable file: %w", ErrTooLarge)

var errMalformed = errors.New("malformed JSON")

// unmarshalQuiet is json.Unmarshal with an error that never carries content.
func unmarshalQuiet(b []byte, v any) error {
	if err := json.Unmarshal(b, v); err != nil {
		return errMalformed
	}
	return nil
}

// readJSON reads path into v. A missing file reports found=false with no error;
// any other failure (unreadable, malformed) returns a non-nil error whose text
// is fixed, so no file content can leak through it.
func readJSON(st Store, path string, v any) (found bool, err error) {
	b, err := st.ReadFile(path)
	if err != nil {
		if isNotExist(err) {
			return false, nil
		}
		if errors.Is(err, ErrTooLarge) {
			return true, errTooLargeFile
		}
		return true, errors.New("unreadable file")
	}
	return true, unmarshalQuiet(b, v)
}

// msTime converts Unix milliseconds; zero or negative is the zero time.
func msTime(ms float64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(int64(ms))
}

// jwtClaims decodes the payload of a JWT without verifying it. It returns nil
// when s is not a JWT.
func jwtClaims(s string) map[string]any {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	return m
}

func claimString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// claimExp is the exp claim as a time; zero when absent.
func claimExp(m map[string]any) time.Time {
	f, ok := m["exp"].(float64)
	if !ok || f <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(f), 0)
}

// openaiAuth returns the "https://api.openai.com/auth" object of claims.
func openaiAuth(m map[string]any) map[string]any {
	a, _ := m[openaiAuthClaim].(map[string]any)
	return a
}

// openaiProfile returns the "https://api.openai.com/profile" object of claims.
func openaiProfile(m map[string]any) map[string]any {
	a, _ := m[openaiProfileClaim].(map[string]any)
	return a
}

// sharingScope is the scope of a ChatGPT login made for the API.
const sharingScope = "chatgpt.tokens.use.direct"

// tokenSharing reports access-token claims that carry the token-sharing scope:
// scp or scope, each as a list of strings or a space-separated string.
func tokenSharing(claims map[string]any) bool {
	return scopeHas(claims["scp"]) || scopeHas(claims["scope"])
}

// scopeHas reports whether a scope claim, a space-separated string or a list
// of strings, holds the token-sharing scope.
func scopeHas(claim any) bool {
	switch v := claim.(type) {
	case string:
		return slices.Contains(strings.Fields(v), sharingScope)
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok && s == sharingScope {
				return true
			}
		}
	}
	return false
}

// markTokenSharing turns a into the account of a token-sharing login: no
// credential, no key, no identity, nothing to query.
func markTokenSharing(a *Account) {
	a.TokenSharing = true
	a.Plan = usage.TokenSharingPlan()
	a.Label = ""
	a.Key = ""
	a.Fingerprints = nil
	a.IdentityKnown = false
	a.Cred = Credential{}
	a.Status = usage.StatusNotVisible
	a.Note = usage.NoteSharingNotVisible
}

// expiredStatus marks an account whose access token has expired.
func expiredStatus(a *Account, now time.Time, harness string) {
	if !a.Expires.IsZero() && a.Expires.Before(now) {
		a.Status = usage.StatusExpired
		a.Note = "token expired, open " + harness + " to refresh"
	}
}

// unreadable is the account of a harness whose credential store cannot be read.
func unreadable(provider, harness string) Account {
	return Account{
		Provider:  provider,
		ID:        harness,
		Harnesses: []string{harness},
		Auth:      AuthNone,
		Plan:      unknownPlan(),
		Status:    usage.StatusUnavailable,
		Note:      noteUnreadable,
	}
}

// stripJSONC removes // and /* */ comments outside strings.
func stripJSONC(in []byte) []byte {
	var out bytes.Buffer
	inStr := false
	for i := 0; i < len(in); i++ {
		c := in[i]
		if inStr {
			out.WriteByte(c)
			switch {
			case c == '\\' && i+1 < len(in):
				i++
				out.WriteByte(in[i])
			case c == '"':
				inStr = false
			}
			continue
		}
		next := byte(0)
		if i+1 < len(in) {
			next = in[i+1]
		}
		switch {
		case c == '"':
			inStr = true
			out.WriteByte(c)
		case c == '/' && next == '/':
			i = skipLineComment(in, i)
			out.WriteByte('\n')
		case c == '/' && next == '*':
			i = skipBlockComment(in, i)
			out.WriteByte(' ')
		default:
			out.WriteByte(c)
		}
	}
	return out.Bytes()
}

// skipLineComment returns the index of the newline ending the comment at i.
func skipLineComment(in []byte, i int) int {
	for i < len(in) && in[i] != '\n' {
		i++
	}
	return i
}

// skipBlockComment returns the index of the '/' closing the comment at i.
func skipBlockComment(in []byte, i int) int {
	for i += 2; i+1 < len(in); i++ {
		if in[i] == '*' && in[i+1] == '/' {
			return i + 1
		}
	}
	return len(in)
}
