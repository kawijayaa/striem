package database

import (
	"math"
	"strings"
	"testing"
)

func TestArraySearchBounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func() (any, error)
	}{
		{"input", func() (any, error) {
			return kqlArrayIndexOf(strings.Repeat("x", maxReplacementBytes+1), int64(1), "long")
		}},
		{"elements", func() (any, error) {
			return kqlArrayIndexOf(`[`+strings.Repeat("0,", maxReplacementParts)+`0]`, int64(0), "long")
		}},
		{"lookup", func() (any, error) {
			return kqlArrayIndexOf("[]", strings.Repeat("x", maxReplacementBytes+1), "string")
		}},
		{"zero occurrence", func() (any, error) { return kqlArrayIndexOf("[1]", int64(1), "long", int64(0), int64(-1), int64(0)) }},
		{"non-scalar", func() (any, error) { return kqlArrayIndexOf("[]", "{}", "dynamic") }},
		{"invalid UTF8 lookup", func() (any, error) { return kqlArrayIndexOf("[]", string([]byte{0xff}), "string") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.run(); err == nil {
				t.Fatal("expected diagnostic")
			}
		})
	}
	for _, tc := range []struct {
		start, length, occurrence int64
		want                      any
	}{
		{math.MinInt64, -1, 1, nil},
		{math.MaxInt64, -1, 1, int64(-1)},
		{0, math.MaxInt64, 1, int64(0)},
		{0, -1, math.MaxInt64, int64(-1)},
	} {
		got, err := kqlArrayIndexOf("[1]", int64(1), "long", tc.start, tc.length, tc.occurrence)
		if err != nil || got != tc.want {
			t.Fatalf("bounds: %v %v", got, err)
		}
	}
	if got, err := kqlArrayIndexOf("[\"\xff\"]", "�", "string"); err != nil || got != nil {
		t.Fatalf("invalid UTF8 array: %v %v", got, err)
	}
}
