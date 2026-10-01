package database

import (
	"database/sql"
	"strings"
	"testing"
)

func TestIndexOfZeroOccurrenceError(t *testing.T) {
	db, err := sql.Open(sqliteDriverName, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var result any
	err = db.QueryRow(`SELECT kql_indexof('abc','a',0,-1,0)`).Scan(&result)
	if err == nil || !strings.Contains(err.Error(), "zero occurrence") {
		t.Fatalf("expected explicit unsupported edge error, got %v", err)
	}
}

func TestIndexOfEmptyAndOverlappingMatches(t *testing.T) {
	// These characterize termination and current behavior, not differential
	// evidence of Kusto semantics for undocumented empty/overlapping matches.
	for _, tc := range []struct {
		text, match string
		options     []any
		want        int64
	}{
		{"", "", nil, 0},
		{"abc", "", []any{int64(0), int64(-1), int64(4)}, 3},
		{"abc", "", []any{int64(0), int64(-1), int64(9223372036854775807)}, -1},
		{"aaaa", "aa", []any{int64(0), int64(-1), int64(3)}, 2},
	} {
		got, err := kqlIndexOf(tc.text, tc.match, tc.options...)
		if err != nil || got != tc.want {
			t.Fatalf("got=%v error=%v want=%d", got, err, tc.want)
		}
	}
}

func TestIndexOfPeriodicPattern(t *testing.T) {
	// A large repeated pattern and late occurrence exercise the formerly
	// quadratic repeated-search case without timing-dependent assertions.
	got, err := kqlIndexOf(strings.Repeat("a", 100000), strings.Repeat("a", 50000), int64(0), int64(-1), int64(50001))
	if err != nil || got != int64(50000) {
		t.Fatalf("got=%v error=%v", got, err)
	}
}
