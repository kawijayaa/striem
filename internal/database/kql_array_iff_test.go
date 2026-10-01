package database

import (
	"strings"
	"testing"
)

func TestArrayIffBounds(t *testing.T) {
	for _, tc := range []struct {
		name                string
		conditions, yes, no any
		yesKind, noKind     string
	}{
		{"condition bytes", strings.Repeat("x", maxReplacementBytes+1), 1, 0, "long", "long"},
		{"condition count", `[` + strings.Repeat("true,", maxReplacementParts) + `true]`, 1, 0, "long", "long"},
		{"branch bytes", "[true]", strings.Repeat("x", maxReplacementBytes+1), 0, "string", "long"},
		{"branch count", "[true]", `[` + strings.Repeat("0,", maxReplacementParts) + `0]`, 0, "dynamic", "long"},
		{"output", "[true,true]", strings.Repeat("x", maxReplacementBytes/2), 0, "string", "long"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := kqlArrayIff(tc.conditions, tc.yes, tc.no, tc.yesKind, tc.noKind); err == nil {
				t.Fatal("expected resource diagnostic")
			}
		})
	}
	// Invalid/null arrays remain characterization pending Kusto evidence.
	for _, value := range []any{nil, "null", "{}", "invalid"} {
		if got, err := kqlArrayIff(value, int64(1), int64(0), "long", "long"); err != nil || got != nil {
			t.Fatalf("invalid condition array: %v %v", got, err)
		}
	}
}
