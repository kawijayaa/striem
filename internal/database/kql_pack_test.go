package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDynamicConstructorLimits(t *testing.T) {
	db, err := sql.Open(sqliteDriverName, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, tc := range []struct {
		query   string
		args    []any
		message string
	}{
		{`SELECT kql_bag_pack('long,long','x',1,'x',2)`, nil, "duplicate"},
		{`SELECT kql_pack_array('string',?)`, []any{strings.Repeat("a", 4<<20)}, "exceeds"},
		{`SELECT kql_pack_array('dynamic',?)`, []any{`"` + strings.Repeat("<", 1<<20) + `"`}, "exceeds"},
		{`SELECT kql_bag_pack('string','x',?)`, []any{strings.Repeat("a", 4<<20)}, "exceeds"},
	} {
		var result any
		err := db.QueryRow(tc.query, tc.args...).Scan(&result)
		if err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Fatalf("expected %s, got %v", tc.message, err)
		}
	}
}

func TestMaximumBagConstructorArity(t *testing.T) {
	db, err := sql.Open(sqliteDriverName, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	query := "SELECT kql_bag_pack(?"
	args := []any{strings.TrimSuffix(strings.Repeat("long,", 64), ",")}
	for i := 0; i < 64; i++ {
		query += ",?,?"
		args = append(args, fmt.Sprintf("k%d", i), int64(i))
	}
	query += ")"
	var result string
	if err := db.QueryRow(query, args...).Scan(&result); err != nil {
		t.Fatal(err)
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result), &properties); err != nil || len(properties) != 64 {
		t.Fatalf("result=%s error=%v", result, err)
	}
}
