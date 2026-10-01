package database

import (
	"database/sql"
	"strings"
	"testing"
)

func TestReplacementResourceLimit(t *testing.T) {
	db, err := sql.Open(sqliteDriverName, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// A small input can amplify a replacement to many megabytes. Query errors
	// must reach callers instead of exhausting memory or truncating output.
	var got string
	replacement := strings.Repeat("x", 1024)
	err = db.QueryRow(`SELECT kql_replace_string(?, 'a', ?)`, strings.Repeat("a", 4096), replacement).Scan(&got)
	if err != nil || len(got) != 4<<20 {
		t.Fatalf("boundary length=%d error=%v", len(got), err)
	}
	err = db.QueryRow(`SELECT kql_replace_string(?, 'a', ?)`, strings.Repeat("a", 4097), replacement).Scan(&got)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected resource error, got %v", err)
	}
	var missing any
	if err := db.QueryRow(`SELECT kql_replace_string(NULL, 'a', 'b')`).Scan(&missing); err != nil || missing != nil {
		t.Fatalf("NULL result=%v error=%v", missing, err)
	}
}

func TestMultipleReplacementErrors(t *testing.T) {
	db, err := sql.Open(sqliteDriverName, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, tc := range []struct{ text, lookups, rewrites, message string }{
		{"a", `{}`, `[]`, "arrays"},
		{"a", `["a"]`, `[]`, "equal lengths"},
		{"a", `["a"] trailing`, `["X"]`, "valid"},
		{"a", `["a"`, `["X"]`, "valid"},
		{strings.Repeat("a", 4097), `["a"]`, `["` + strings.Repeat("x", 1024) + `"]`, "result exceeds"},
		{strings.Repeat("a", 65537), `["a"]`, `["x"]`, "match limit"},
		{strings.Repeat("a", 1<<20), `[` + strings.TrimSuffix(strings.Repeat(`"z",`, 65), ",") + `]`, `[` + strings.TrimSuffix(strings.Repeat(`"x",`, 65), ",") + `]`, "work limit"},
	} {
		var result any
		err := db.QueryRow(`SELECT kql_replace_strings(?,?,?)`, tc.text, tc.lookups, tc.rewrites).Scan(&result)
		if err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Fatalf("expected %s, got %v", tc.message, err)
		}
	}
}
