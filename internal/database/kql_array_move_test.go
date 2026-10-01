package database

import (
	"math"
	"strings"
	"testing"
)

func TestArrayMovementExtremeCounts(t *testing.T) {
	for _, tc := range []struct {
		count         int64
		right         bool
		rotate, shift string
	}{
		{math.MinInt64, false, "[2,3,1]", "[null,null,null]"},
		{math.MinInt64, true, "[3,1,2]", "[null,null,null]"},
		{math.MaxInt64, false, "[2,3,1]", "[null,null,null]"},
		{math.MaxInt64, true, "[3,1,2]", "[null,null,null]"},
		{-3, false, "[1,2,3]", "[null,null,null]"},
	} {
		got, err := kqlArrayRotate("[1,2,3]", tc.count, tc.right)
		if err != nil || got != tc.rotate {
			t.Fatalf("rotate(%d,%v): %v %v", tc.count, tc.right, got, err)
		}
		got, err = kqlArrayShift("[1,2,3]", tc.count, nil, "__array_default", tc.right)
		if err != nil || got != tc.shift {
			t.Fatalf("shift(%d,%v): %v %v", tc.count, tc.right, got, err)
		}
	}
}

func TestArrayMovementResourceBounds(t *testing.T) {
	for _, value := range []string{strings.Repeat("x", maxReplacementBytes+1), `[` + strings.Repeat("0,", maxReplacementParts) + `0]`} {
		if _, err := kqlArrayRotate(value, int64(1), false); err == nil {
			t.Fatal("expected rotation input diagnostic")
		}
		if _, err := kqlArrayShift(value, int64(1), nil, "__array_default", true); err == nil {
			t.Fatal("expected shift input diagnostic")
		}
	}
	if _, err := kqlArrayShift("[1]", int64(1), strings.Repeat("x", maxReplacementBytes+1), "string", false); err == nil {
		t.Fatal("expected padding size diagnostic")
	}
	if _, err := kqlArrayShift("[1,2]", int64(2), strings.Repeat("x", maxReplacementBytes/2), "string", false); err == nil {
		t.Fatal("expected output size diagnostic")
	}
	for _, value := range []string{`["a","b"]`, `[1,"a",null]`} {
		if _, err := kqlArrayShift(value, int64(1), nil, "__array_default", false); err == nil {
			t.Fatal("expected unsupported string default diagnostic")
		}
	}
	// Invalid count/input behavior is characterized pending differential evidence.
	for _, value := range []any{nil, "null", "{}", "invalid"} {
		if got, err := kqlArrayRotate(value, int64(1), false); err != nil || got != nil {
			t.Fatalf("invalid rotation: %v %v", got, err)
		}
		if got, err := kqlArrayShift(value, int64(1), nil, "__array_default", true); err != nil || got != nil {
			t.Fatalf("invalid shift: %v %v", got, err)
		}
	}
	for _, count := range []any{nil, float64(1), "1"} {
		if got, err := kqlArrayRotate("[1]", count, false); err != nil || got != nil {
			t.Fatalf("invalid rotation count: %v %v", got, err)
		}
		if got, err := kqlArrayShift("[1]", count, nil, "__array_default", true); err != nil || got != nil {
			t.Fatalf("invalid shift count: %v %v", got, err)
		}
	}
}
