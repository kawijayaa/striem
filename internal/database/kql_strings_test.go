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
