package database

import (
	"math"
	"strings"
	"testing"
)

func TestHashScalarRepresentations(t *testing.T) {
	for _, algorithm := range []string{"md5", "sha1", "sha256"} {
		vectors := map[string]string{"md5": "59a0e289cbbfb2a4c621126dc4d465d5", "sha1": "090e4768b2b92da7864dc6561dc1d17a972db72c", "sha256": "8cf55c5c4856286bfebeedef3662d2f69ffcb06995ea7b996ea99f8c5c4efe47"}
		if got, err := hashKQLScalar("é😀\x00", "string", algorithm); err != nil || got != vectors[algorithm] {
			t.Fatalf("Unicode vector: %v %v", got, err)
		}
		date, err := hashKQLScalar("2020-01-01T02:00:00+02:00", "datetime", algorithm)
		canonical, err2 := hashKQLScalar("2020-01-01T00:00:00.0000000Z", "string", algorithm)
		if err != nil || err2 != nil || date != canonical {
			t.Fatal("datetime UTC canonicalization changed")
		}
		for _, text := range []string{"é😀\x00", strings.Repeat("x", maxHashInputBytes)} {
			if got, err := hashKQLScalar(text, "string", algorithm); err != nil || got == nil {
				t.Fatalf("valid input: %v", err)
			}
		}
		for _, tc := range []struct {
			value any
			kind  string
		}{
			{"0001-01-01T00:00:00+01:00", "datetime"}, {"9999-12-31T23:59:59-01:00", "datetime"}, {"bad", "datetime"}, {"2020-01-01T00:00:00.000000001Z", "datetime"}, {int64(2), "bool"}, {math.Inf(1), "real"}, {strings.Repeat("a", maxHashInputBytes+1), "string"},
		} {
			if got, err := hashKQLScalar(tc.value, tc.kind, algorithm); err == nil || got != nil {
				t.Fatalf("unsupported value accepted: %s", tc.kind)
			}
		}
		if got, err := hashKQLScalar(string([]byte{255}), "string", algorithm); err != nil || got != nil {
			t.Fatal("invalid UTF-8 accepted")
		}
	}
}
