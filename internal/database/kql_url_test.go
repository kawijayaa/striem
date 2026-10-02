package database

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestURLParsingBoundsAndMalformedInputs(t *testing.T) {
	for _, parse := range []func(any) (any, error){kqlParseURL, kqlParseURLQuery} {
		for _, value := range []any{strings.Repeat("a", maxDecodeInputBytes+1), string([]byte{0xff}), int64(1)} {
			if _, err := parse(value); err == nil {
				t.Fatal("missing input diagnostic")
			}
		}
		for _, value := range []any{nil, "%zz", "https://host/?a=%ff"} {
			got, err := parse(value)
			if got != nil || err != nil {
				t.Fatalf("malformed input %v: %v %v", value, got, err)
			}
		}
	}
	for _, value := range []string{"host/path", "http://[bad", "http://host:bad", "http://host/%ff"} {
		got, err := kqlParseURL(value)
		if got != nil || err != nil {
			t.Fatalf("malformed URL %q: %v %v", value, got, err)
		}
	}
}

// These edge cases characterize the local policy; Microsoft engine comparison remains open.
func TestURLQueryEncodingAndDuplicateKeys(t *testing.T) {
	got, err := kqlParseURLQuery("x=first&x=last&space=hello+world&plus=%2B&unicode=%C3%A9&blank=&bare")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]map[string]string
	if err := json.Unmarshal([]byte(got.(string)), &decoded); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"x": "last", "space": "hello world", "plus": "+", "unicode": "é", "blank": "", "bare": ""}
	if !reflect.DeepEqual(decoded["Query Parameters"], want) {
		t.Fatalf("got %v want %v", decoded, want)
	}
	maximum := "key=" + strings.Repeat("a", maxDecodeInputBytes-4)
	if _, err := kqlParseURLQuery(maximum); err != nil {
		t.Fatal(err)
	}
}

func TestURLComponentCharacterization(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  map[string]any
	}{
		{"https://host", map[string]any{"Host": "host", "Port": "", "Path": "", "Username": "", "Password": "", "Fragment": ""}},
		{"http://[::1]:80/a%20b#c%20d", map[string]any{"Host": "::1", "Port": "80", "Path": "a b", "Fragment": "c d"}},
		{"https://u%20s:p%2Bass@host//path", map[string]any{"Username": "u s", "Password": "p+ass", "Path": "/path"}},
	} {
		got, err := kqlParseURL(tc.input)
		if err != nil || got == nil {
			t.Fatalf("%s: %v %v", tc.input, got, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(got.(string)), &decoded); err != nil {
			t.Fatal(err)
		}
		for key, want := range tc.want {
			if decoded[key] != want {
				t.Fatalf("%s: %s=%v want=%v", tc.input, key, decoded[key], want)
			}
		}
	}
}
