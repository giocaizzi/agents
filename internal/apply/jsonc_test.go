package apply

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestStripJSONC(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, in, want string }{
		{"line comment", "{\"a\":1} // c\n", "{\"a\":1} \n"},
		{"block comment", "{/* c */\"a\":1}", "{ \"a\":1}"},
		{"multiline block keeps newlines", "{/*a\nb*/}", "{ \n}"[:0] + "{\n }"},
		{"trailing commas", "{\"a\":[1,2,\n],}", "{\"a\":[1,2\n]}"},
		{"trailing comma before comment", "[1, // x\n]", "[1 \n]"},
		{"comment markers in strings", `{"u":"http://x/*y*/"}`, `{"u":"http://x/*y*/"}`},
		{"escaped quote in string", `{"a":"\" // not a comment"}`, `{"a":"\" // not a comment"}`},
		{"unterminated block", "{/* open", "{ "},
		{"comma in string kept", `["a,]"]`, `["a,]"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, string(stripJSONC([]byte(tt.in)))); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}

func FuzzStripJSONC(f *testing.F) {
	for _, s := range []string{
		`{"a":1}`, "{// c\n\"a\":[1,],}", `{"a":"/* x */"}`, "/*", "//", `"\`, `{"a":"\\"}`, "[1,,]", "",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		out := stripJSONC(in)
		if len(out) > len(in)+1 {
			t.Fatalf("output grew: %d -> %d", len(in), len(out))
		}
		// Valid JSON has no comments or trailing commas, so it must pass through untouched.
		if json.Valid(in) && string(out) != string(in) {
			t.Fatalf("valid json changed: %q -> %q", in, out)
		}
	})
}
