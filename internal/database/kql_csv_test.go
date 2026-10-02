package database

import (
	"strings"
	"testing"
)

func TestParseCSVRuntimeBounds(t *testing.T) {
	for _, value := range []any{int64(1), string([]byte{0xff}), strings.Repeat("a", maxDynamicInputBytes+1), strings.Repeat("\x00", maxDynamicInputBytes)} {
		if _, err := kqlParseCSV(value); err == nil {
			t.Fatal("expected input or encoded output diagnostic")
		}
	}
	if got, err := kqlParseCSV(strings.Repeat("a", maxDynamicInputBytes)); err != nil || got == nil {
		t.Fatalf("maximum input: %v %v", got != nil, err)
	}
	if got, err := kqlParseCSV(nil); got != nil || err != nil {
		t.Fatalf("null: %v %v", got, err)
	}
}

// Policies beyond Microsoft's examples require engine differential verification.
func TestParseCSVEdgeCharacterization(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", `[""]`}, {",,", `["","",""]`}, {" a, b ", `[" a"," b "]`},
		{"é,\x00", `["é","\u0000"]`}, {"a,b\r\nc,d", `["a","b"]`},
		{"\"a\r\nb\",c", `["a\nb","c"]`}, {"a,b\n\"bad", `["a","b"]`},
	} {
		got, err := kqlParseCSV(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("%q: got=%v err=%v want=%s", tc.input, got, err, tc.want)
		}
	}
	for _, input := range []string{`a,"unterminated`, `a"b,c`, `"a"tail,c`} {
		if got, err := kqlParseCSV(input); got != nil || err != nil {
			t.Fatalf("malformed %q: %v %v", input, got, err)
		}
	}
}
