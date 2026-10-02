package database

import (
	"strings"
	"testing"
)

func TestExtractAllValidationAndLimits(t *testing.T) {
	for _, tc := range []struct{ pattern, groups, source any }{
		{"(a)", "[]", "a"}, {"(a)", "[0]", "a"}, {"(a)", "[2]", "a"},
		{"(a)", `["missing"]`, "a"}, {"(a)", "[true]", "a"}, {"(a)", "[1.5]", "a"}, {"(a)", "{}", "a"},
		{"(a)", "[0]", "no match"}, {"(a)", "[null]", "a"},
		{"a", nil, "a"}, {strings.Repeat("(a)", 17), nil, "a"}, {"(", nil, "a"},
		{strings.Repeat("a", 513), nil, "a"}, {"(a)", nil, strings.Repeat("a", maxReplacementBytes+1)},
		{"(a)", nil, string([]byte{0xff})}, {1, nil, "a"},
		{"()", nil, strings.Repeat("a", maxReplacementParts)},
		{"(.*)", nil, strings.Repeat(">", maxReplacementBytes)},
	} {
		if _, err := kqlExtractAll(tc.pattern, tc.groups, tc.source); err == nil {
			t.Fatalf("missing validation: pattern=%v groups=%v", tc.pattern, tc.groups)
		}
	}
	if got, err := kqlExtractAll("(a)", nil, nil); got != nil || err != nil {
		t.Fatalf("null: %v %v", got, err)
	}
	if got, err := kqlExtractAll("(a)", nil, strings.Repeat("a", maxReplacementParts)); got == nil || err != nil {
		t.Fatalf("exact match boundary: present=%v err=%v", got != nil, err)
	}
}

func TestExtractAllEdgeCharacterization(t *testing.T) {
	for _, tc := range []struct {
		pattern      string
		groups       any
		source, want string
	}{
		{"(a)?(b)", nil, "b ab", `[["","b"],["a","b"]]`},
		{"(a)(b)", "[2,1,2]", "ab", `[["b","a","b"]]`},
		{"()", nil, "é", `["",""]`},
		{"(.)", nil, "é\x00", `["é","\u0000"]`},
	} {
		if got, err := kqlExtractAll(tc.pattern, tc.groups, tc.source); got != tc.want || err != nil {
			t.Fatalf("%s: %v %v want=%s", tc.pattern, got, err, tc.want)
		}
	}
}
