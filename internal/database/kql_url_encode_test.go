package database

import (
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestURLEncoding(t *testing.T) {
	for _, tc := range []struct{ text, form, component string }{
		{"https://www.bing.com/hello world", "https%3a%2f%2fwww.bing.com%2fhello+world", "https%3a%2f%2fwww.bing.com%2fhello%20world"},
		{"https://www.bing.com/hello world/", "https%3a%2f%2fwww.bing.com%2fhello+world%2f", "https%3a%2f%2fwww.bing.com%2fhello%20world%2f"},
		{"a+b &=%/", "a%2bb+%26%3d%25%2f", "a%2bb%20%26%3d%25%2f"},
		{"é😀\x00", "%c3%a9%f0%9f%98%80%00", "%c3%a9%f0%9f%98%80%00"},
		{"AZaz09-._!*()~'", "AZaz09-._!*()%7e%27", "AZaz09-._!*()%7e%27"},
		{"", "", ""},
	} {
		if got := kqlURLEncode(tc.text); got != tc.form {
			t.Fatalf("form %q: got %v want %s", tc.text, got, tc.form)
		}
		if got := kqlURLEncodeComponent(tc.text); got != tc.component {
			t.Fatalf("component %q: got %v want %s", tc.text, got, tc.component)
		}
	}
	for _, component := range []bool{false, true} {
		for _, bad := range []any{nil, 1, []byte("text"), string([]byte{255}), strings.Repeat("a", maxDecodeInputBytes+1)} {
			if encodeKQLURL(bad, component) != nil {
				t.Fatal("invalid input accepted")
			}
		}
	}
	text := strings.Repeat("\x00", maxDecodeInputBytes)
	encoded := kqlURLEncodeComponent(text)
	if encoded == nil || len(encoded.(string)) != 3*maxDecodeInputBytes || kqlURLDecode(encoded) != text {
		t.Fatal("maximum encoded expansion did not round trip")
	}
	if kqlURLDecode(strings.Repeat("%00", maxDecodeInputBytes+1)) != nil {
		t.Fatal("encoded budget ignored")
	}
	if kqlURLDecode(strings.Repeat("a", maxDecodeInputBytes+1)) != nil {
		t.Fatal("decoded budget ignored")
	}
}

func FuzzURLEncoding(f *testing.F) {
	f.Add("https://example.org/a b?x=é+😀")
	f.Add("\x00")
	f.Fuzz(func(t *testing.T, text string) {
		for _, component := range []bool{false, true} {
			encoded := encodeKQLURL(text, component)
			if !utf8.ValidString(text) || len(text) > maxDecodeInputBytes {
				if encoded != nil {
					t.Fatal("invalid input encoded")
				}
				continue
			}
			if encoded == nil {
				t.Fatal("valid input rejected")
			}
			var decoded string
			var err error
			if component {
				decoded, err = url.PathUnescape(encoded.(string))
				if kqlURLDecode(encoded) != text {
					t.Fatal("runtime component roundtrip failed")
				}
			} else {
				decoded, err = url.QueryUnescape(encoded.(string))
			}
			if err != nil || decoded != text {
				t.Fatal("reference decoder roundtrip failed")
			}
			if len(encoded.(string)) > 3*len(text) {
				t.Fatal("expansion exceeds bound")
			}
		}
	})
}
