package database

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCharacterStringValidation(t *testing.T) {
	for _, tc := range []struct{ search, replacement, source, want string }{
		{"aba", "xyz", "aba", "xyx"}, {"a", "b", "aabb", "bbbb"}, {"a", "", "cat", "ct"}, {"a", "😀", "a\x00a", "😀\x00😀"},
	} {
		if got, err := kqlTranslate(tc.search, tc.replacement, tc.source); err != nil || got != tc.want {
			t.Fatalf("translation: %v %v", got, err)
		}
	}
	for _, value := range []any{nil, 1, string([]byte{255})} {
		if got, err := kqlTranslate("a", "b", value); err != nil || got != nil {
			t.Fatal("invalid translate input accepted")
		}
	}
	for _, text := range []string{"", "é😀\x00", strings.Repeat("a", maxCharacterStringBytes)} {
		reversed, err := kqlReverse(text, "string")
		if err != nil {
			t.Fatal(err)
		}
		restored, err := kqlReverse(reversed, "string")
		if err != nil || restored != text {
			t.Fatal("reverse round trip")
		}
	}
	for _, tc := range [][3]string{{"abc", "xy", "abc"}, {strings.Repeat("a", maxTranslationAlphabetBytes+1), "x", "a"}, {"a", "😀", strings.Repeat("a", maxCharacterStringBytes/4+1)}} {
		if got, err := kqlTranslate(tc[0], tc[1], tc[2]); err == nil || got != nil {
			t.Fatal("unsupported translation or budget accepted")
		}
	}
	if got, err := kqlReverse(strings.Repeat("a", maxCharacterStringBytes+1), "string"); err == nil || got != nil {
		t.Fatal("reverse budget missing")
	}
	if got, err := kqlReverse(string([]byte{255}), "string"); err != nil || got != nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}

func FuzzCharacterStringTransformations(f *testing.F) {
	f.Add("abc😀é\x00", "a😀", "xy")
	f.Add("spark", "krasp", "otsku")
	f.Fuzz(func(t *testing.T, source, search, replacement string) {
		if len(source) > 65536 || len(search) > 1024 || len(replacement) > 1024 {
			return
		}
		if utf8.ValidString(source) {
			reversed, err := kqlReverse(source, "string")
			if err != nil {
				t.Fatal(err)
			}
			original, err := kqlReverse(reversed, "string")
			if err != nil || original != source {
				t.Fatal("reverse round trip failed")
			}
		}
		result, err := kqlTranslate(search, replacement, source)
		if err != nil || result == nil {
			return
		}
		if !utf8.ValidString(source) || !utf8.ValidString(search) || !utf8.ValidString(replacement) {
			t.Fatal("invalid UTF-8 accepted")
		}
		points, rewrites := []rune(search), []rune(replacement)
		var reference strings.Builder
		for _, point := range source {
			index := -1
			for i, candidate := range points {
				if point == candidate {
					index = i
					break
				}
			}
			if index < 0 {
				reference.WriteRune(point)
			} else if len(rewrites) == 1 {
				reference.WriteRune(rewrites[0])
			} else if len(rewrites) > 0 {
				reference.WriteRune(rewrites[index])
			}
		}
		if result != reference.String() {
			t.Fatal("translation differs from positional reference")
		}
	})
}
