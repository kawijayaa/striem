package database

import (
	"database/sql"
	"strings"
	"testing"
)

func TestRegexReplacementLimits(t *testing.T) {
	db, err := sql.Open(sqliteDriverName, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, tc := range []struct{ source, pattern, rewrite, message string }{
		{"abc", "b", `\9`, "capture group"},
		{strings.Repeat("a", 65537), "a", "X", "match limit"},
		{strings.Repeat("a", 4097), "a", strings.Repeat("x", 1024), "result exceeds"},
		{"a", strings.Repeat("a", 513), "X", "pattern exceeds"},
		{strings.Repeat("a", 4096), "a", strings.Repeat(`\0`, 20000), "work limit"},
	} {
		var result any
		err := db.QueryRow(`SELECT kql_replace_regex(?,?,?)`, tc.source, tc.pattern, tc.rewrite).Scan(&result)
		if err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Fatalf("expected %s, got %v", tc.message, err)
		}
	}
}
