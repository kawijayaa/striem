package database

import (
	"strings"
	"testing"
)

func TestParseJSONRuntimeBounds(t *testing.T) {
	for _, value := range []any{strings.Repeat("x", maxReplacementBytes+1), strings.Repeat("\x00", maxReplacementBytes/4), string([]byte{0xff}), int64(1)} {
		if _, err := kqlParseJSON(value); err == nil {
			t.Fatal("expected input or encoding diagnostic")
		}
	}
	got, err := kqlParseJSON(nil)
	if err != nil || got != nil {
		t.Fatalf("null input: %v %v", got, err)
	}
	got, err = kqlParseJSON(` { "n" : 9007199254740993 } `)
	if err != nil || got != `{"n":9007199254740993}` {
		t.Fatalf("long precision: %v %v", got, err)
	}
	got, err = kqlParseJSON(`{"broken":`)
	if err != nil || got != `"{\"broken\":"` {
		t.Fatalf("invalid JSON preservation: %v %v", got, err)
	}
}
