package database

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestUnicodeCodepointValidation(t *testing.T) {
	for _, tc := range []struct{ array, want string }{
		{`[0,127,128,2047,2048,65535,65536,1114111]`, "\x00\x7f\u0080\u07ff\u0800\uffff\U00010000\U0010ffff"},
		{`[55295,57344]`, "\ud7ff\ue000"}, {`[]`, ""},
	} {
		if got, err := kqlCodepointsToString(tc.array); err != nil || got != tc.want {
			t.Fatalf("valid points %s: %v %v", tc.array, got, err)
		}
	}
	for _, input := range []any{nil, "null", "{}", `[null]`, `[true]`, `["75"]`, `[[75]]`, `[-1]`, `[1114112]`, `[55296]`, `[56320]`, `[57343]`, `[75.0]`, `[75e0]`, `[9223372036854775807]`} {
		if got, err := kqlCodepointsToString(input); err != nil || got != nil {
			t.Fatalf("invalid input: %v %v %v", input, got, err)
		}
	}
	for _, input := range []any{nil, 1, string([]byte{255})} {
		if got, err := kqlCodepointsFromString(input); err != nil || got != nil {
			t.Fatal("invalid string accepted")
		}
	}
	for _, text := range []string{strings.Repeat("a", maxUnicodeCodepoints), strings.Repeat("😀", maxUnicodeCodepoints)} {
		array, err := kqlCodepointsFromString(text)
		if err != nil {
			t.Fatal(err)
		}
		output, err := kqlCodepointsToString(array)
		if err != nil || output != text {
			t.Fatal("boundary roundtrip failed")
		}
	}
	for _, text := range []string{strings.Repeat("a", maxUnicodeCodepoints+1), strings.Repeat("😀", maxUnicodeCodepoints+1)} {
		if got, err := kqlCodepointsFromString(text); err == nil || got != nil {
			t.Fatal("input limit missing")
		}
	}
	if got, err := kqlCodepointsToString("[" + strings.Repeat("75,", maxUnicodeCodepoints) + "75]"); err == nil || got != nil {
		t.Fatal("array limit missing")
	}
}

func FuzzUnicodeCodepointRoundTrip(f *testing.F) {
	f.Add("😀é\x00")
	f.Add("קוסטו - Kusto")
	f.Add("")
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 4*maxUnicodeCodepoints {
			return
		}
		array, err := kqlCodepointsFromString(text)
		if !utf8.ValidString(text) {
			if array != nil {
				t.Fatal("invalid UTF-8 accepted")
			}
			return
		}
		if utf8.RuneCountInString(text) > maxUnicodeCodepoints {
			if err == nil {
				t.Fatal("missing budget error")
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		output, err := kqlCodepointsToString(array)
		if err != nil || output != text {
			t.Fatal("codepoint roundtrip failed")
		}
	})
}
