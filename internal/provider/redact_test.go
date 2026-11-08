package provider

import (
	"fmt"
	"strings"
	"testing"
)

func TestCredentialFormatEveryVerb(t *testing.T) {
	t.Parallel()
	const secret = "fake-token-1"
	c := NewCredential(secret, "acct-1")
	a := Account{Provider: "anthropic", Cred: c}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%t", "%p", "%10v", "%-10s", "%.2s", "% x", "%#x", "%08d"} {
		got := fmt.Sprintf(verb, c)
		// %p is the one verb fmt handles before Format: a bad verb that
		// prints an address, so only the token's absence is asserted.
		if strings.Contains(got, secret) || (verb != "%p" && got != "[redacted]") {
			t.Errorf("Sprintf(%q, Credential) = %q", verb, got)
		}
		if out := fmt.Sprintf(verb, a); strings.Contains(out, secret) {
			t.Errorf("Sprintf(%q, Account) leaks the token: %q", verb, out)
		}
	}
	// %T prints the type name, never a value.
	if got := fmt.Sprintf("%T", c); got != "provider.Credential" {
		t.Errorf("%%T = %q", got)
	}
	if out := fmt.Sprintf("%T", a); strings.Contains(out, secret) {
		t.Errorf("%%T(Account) leaks the token: %q", out)
	}
}
