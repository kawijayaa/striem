package database

import (
	"database/sql"
	"strings"
	"testing"
)

func TestTrimBoundsAndNulls(t *testing.T) {
	db, err := sql.Open(sqliteDriverName, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, name := range []string{"kql_trim", "kql_trim_start", "kql_trim_end"} {
		var result any
		if err := db.QueryRow("SELECT " + name + "('x',NULL)").Scan(&result); err != nil || result != nil {
			t.Fatalf("null result=%v error=%v", result, err)
		}
		err := db.QueryRow("SELECT "+name+"('x',?)", strings.Repeat("a", (4<<20)+1)).Scan(&result)
		if err == nil || !strings.Contains(err.Error(), "input exceeds") {
			t.Fatalf("expected size error, got %v", err)
		}
	}
}
