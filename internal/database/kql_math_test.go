package database

import (
	"math"
	"testing"
)

func TestMathUDFsThroughSQLite(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, tc := range []struct {
		query string
		want  any
	}{
		{"SELECT kql_max_of(NULL, 2)", int64(2)},
		{"SELECT kql_min_of(3, NULL, 1)", int64(1)},
		{"SELECT kql_max_of(NULL, NULL)", nil},
		{"SELECT kql_max_of('b', 'a')", "b"},
		{"SELECT kql_max_of(1, 2.5)", 2.5},
		{"SELECT kql_isutf8(NULL)", nil},
		{"SELECT kql_isascii(NULL)", nil},
		{"SELECT kql_strcmp(NULL, 'a')", nil},
		{"SELECT kql_math('log2', NULL)", nil},
		{"SELECT kql_number_class('isnan', NULL)", int64(0)},
	} {
		var got any
		if err := store.DB().QueryRow(tc.query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if bytes, ok := got.([]byte); ok {
			got = string(bytes)
		}
		if got != tc.want {
			t.Fatalf("%s = %#v, want %#v", tc.query, got, tc.want)
		}
	}
	if kqlNumberClass("isnan", math.NaN()) != true || kqlNumberClass("isinf", math.Inf(-1)) != true || kqlNumberClass("isfinite", math.Inf(1)) != false {
		t.Fatal("nonfinite classification")
	}
}
