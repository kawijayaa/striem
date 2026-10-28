package database

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStringJoinValidationAndLimits(t *testing.T) {
	if got, err := kqlStrcatDelim("|", "a", []byte(nil), "b"); err != nil || got != "a||b" {
		t.Fatal("scalar join null policy")
	}
	if got, err := kqlStrcatDelim("|", strings.Repeat("x", maxCharacterStringBytes), "x"); err == nil || got != nil {
		t.Fatal("scalar join output budget")
	}
	if got, err := kqlJoinScalarText("2020-01-01T00:00:00.000000001Z", "datetime"); err == nil || got != nil {
		t.Fatal("unsupported datetime precision accepted")
	}
	if got, err := kqlJoinScalarText("2020-01-01T02:00:00+02:00", "datetime"); err != nil || got != "2020-01-01T00:00:00.0000000Z" {
		t.Fatal("canonical datetime conversion failed")
	}
	for _, input := range []any{nil, "null", "{}", "1", "[invalid]"} {
		if got, err := kqlStrcatArray(input, ","); err != nil || got != nil {
			t.Fatalf("invalid array: %v %v", got, err)
		}
	}
	if got, err := kqlStrcatArray(`[true,false,null,{ "x" : [1,2] }]`, "|"); err != nil || got != `true|false||{"x":[1,2]}` {
		t.Fatalf("structured value policy: %v %v", got, err)
	}
	if got, err := kqlStrrep("x", int64(67108864), ""); err != nil || got != strings.Repeat("x", 1024) {
		t.Fatal("repeat cap changed")
	}
	for _, count := range []any{nil, int64(-1), int64(0), int64(67108865), int64(9223372036854775807), float64(2)} {
		if got, err := kqlStrrep("x", count, ","); err != nil || got != nil {
			t.Fatal("invalid multiplier accepted")
		}
	}
	if got, err := kqlStrrep(strings.Repeat("x", maxCharacterStringBytes/2), int64(2), ""); err != nil || len(got.(string)) != maxCharacterStringBytes {
		t.Fatal("exact output boundary failed")
	}
	if got, err := kqlStrrep(strings.Repeat("x", maxCharacterStringBytes/2), int64(2), ","); err == nil || got != nil {
		t.Fatal("replication output bound missing")
	}
	if got, err := kqlStrcatArray(`["a","b"]`, strings.Repeat("x", maxCharacterStringBytes)); err == nil || got != nil {
		t.Fatal("array join output bound missing")
	}
	for _, input := range []any{nil, string([]byte{255})} {
		if got, err := kqlStrrep(input, int64(2), ","); err != nil || got != nil {
			t.Fatal("invalid string accepted")
		}
	}
}

func FuzzStringJoining(f *testing.F) {
	f.Add("😀\x00", "é", int64(3))
	f.Add("", "-", int64(2))
	f.Fuzz(func(t *testing.T, text, delimiter string, count int64) {
		if len(text) > 1024 || len(delimiter) > 1024 || count < 1 || count > 128 {
			return
		}
		if !utf8.ValidString(text) || !utf8.ValidString(delimiter) {
			return
		}
		values := make([]string, int(count))
		for i := range values {
			values[i] = text
		}
		want := strings.Join(values, delimiter)
		got, err := kqlStrrep(text, count, delimiter)
		if err != nil || got != want {
			t.Fatal("replication differs from independent join")
		}
		if count >= 2 && count <= 64 {
			args := make([]any, len(values))
			for i, value := range values {
				args[i] = value
			}
			scalar, err := kqlStrcatDelim(delimiter, args...)
			if err != nil || scalar != want {
				t.Fatal("scalar delimiter join differs from reference")
			}
		}
		raw, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		joined, err := kqlStrcatArray(string(raw), delimiter)
		if err != nil || joined != want {
			t.Fatal("array join differs from string join")
		}
	})
}
