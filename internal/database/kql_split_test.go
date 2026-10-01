package database

import (
	"database/sql"
	"strings"
	"testing"
)

func TestSplitLimitsAndIndexing(t *testing.T) {
	db, err := sql.Open(sqliteDriverName, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, tc := range []struct{ source, delimiter, message string }{
		{"abc", "", "delimiter cannot be empty"},
		{strings.Repeat("a,", 65536), ",", "element limit"},
		{strings.Repeat("\x00", 1<<20), ",", "result exceeds"},
		{strings.Repeat("a", (4<<20)+1), ",", "input exceeds"},
	} {
		var result any
		err := db.QueryRow(`SELECT kql_split(?,?)`, tc.source, tc.delimiter).Scan(&result)
		if err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Fatalf("expected %s, got %v", tc.message, err)
		}
	}
	// Selecting one element does not allocate a complete array or hit its
	// element limit. Undocumented negative/out-of-range behavior is characterized.
	for _, tc := range []struct {
		source string
		index  int64
		want   string
	}{
		{strings.Repeat("a,", 65536), 65536, `[""]`},
		{"a,b", -1, `[]`},
		{"a,b", 9223372036854775807, `[]`},
	} {
		var result string
		err := db.QueryRow(`SELECT kql_split(?,',',?)`, tc.source, tc.index).Scan(&result)
		if err != nil || result != tc.want {
			t.Fatalf("result=%q error=%v want=%q", result, err, tc.want)
		}
	}
}
