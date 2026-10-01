package database

import (
	"strings"
	"testing"
)

func TestSetEqualsResourceBounds(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"input", strings.Repeat("x", maxReplacementBytes+1)},
		{"elements", `[` + strings.Repeat("0,", maxReplacementParts) + `0]`},
		{"depth", `[` + strings.Repeat("[", maxSetKeyDepth+1) + `0` + strings.Repeat("]", maxSetKeyDepth+1) + `]`},
		{"duplicate properties", `[{"a":1,"a":2}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, pair := range [][2]any{{tc.value, "[]"}, {"[]", tc.value}} {
				if _, err := kqlSetEquals(pair[0], pair[1]); err == nil {
					t.Fatal("expected diagnostic, including when unequal cardinalities could shortcut")
				}
			}
		})
	}
	// Non-array/null handling remains characterization pending engine evidence.
	for _, value := range []any{nil, "null", "{}", "invalid", int64(1)} {
		for _, pair := range [][2]any{{value, "[]"}, {"[]", value}} {
			if got, err := kqlSetEquals(pair[0], pair[1]); err != nil || got != nil {
				t.Fatalf("invalid input: got=%v err=%v", got, err)
			}
		}
	}
}
