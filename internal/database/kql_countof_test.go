package database

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCountOfValidationAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		source, search, kind string
		want                 int64
	}{
		{"aaaaa", "aaa", "normal", 3}, {"aaaaa", "aaa", "regex", 1}, {"Aaa", "a", "normal", 2},
		{"abc", "", "normal", 0}, {"", "", "normal", 0}, {"😀é", "", "regex", 3}, {"\x00a\x00", "\x00", "normal", 2},
		{strings.Repeat("a", maxCharacterStringBytes), strings.Repeat("a", maxParseSource), "normal", int64(maxCharacterStringBytes - maxParseSource + 1)},
		{strings.Repeat("a", maxParseSource), "", "regex", int64(maxParseSource + 1)},
	} {
		if got, err := kqlCountOf(tc.source, tc.search, tc.kind); err != nil || got != tc.want {
			t.Fatalf("kind=%s got %v want %d (%v)", tc.kind, got, tc.want, err)
		}
	}
	for _, tc := range [][3]string{{"abc", "a", "Normal"}, {"abc", "[", "regex"}, {strings.Repeat("a", maxParseSource+1), "a", "regex"}, {"a", strings.Repeat("a", maxParsePattern+1), "regex"}, {strings.Repeat("a", maxCharacterStringBytes+1), "a", "normal"}} {
		if got, err := kqlCountOf(tc[0], tc[1], tc[2]); err == nil || got != nil {
			t.Fatal("invalid kind/regex/budget accepted")
		}
	}
	for _, source := range []any{nil, 1, string([]byte{255})} {
		if got, err := kqlCountOf(source, "a", "normal"); err != nil || got != nil {
			t.Fatal("invalid source accepted")
		}
	}
}

func FuzzCountOfPlainMatches(f *testing.F) {
	f.Add("ababa", "aba")
	f.Add("😀😀😀", "😀😀")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, source, search string) {
		if len(source) > 2048 || len(search) > 256 {
			return
		}
		got, err := kqlCountOf(source, search, "normal")
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.ValidString(source) || !utf8.ValidString(search) {
			if got != nil {
				t.Fatal("invalid UTF-8 accepted")
			}
			return
		}
		want := int64(0)
		if search != "" {
			for index := range source {
				if strings.HasPrefix(source[index:], search) {
					want++
				}
			}
		}
		if got != want {
			t.Fatalf("overlap count got %v want %d", got, want)
		}
	})
}
