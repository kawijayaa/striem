package database

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

func TestBagAggregateRuntime(t *testing.T) {
	db, err := sql.Open("striem_sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got string
	err = db.QueryRow(`WITH vals(x) AS (VALUES ('{"a":1}'),('{"a":2,"b":3}'),(NULL),('not json'),('[]')) SELECT kql_make_bag(x,1048576) FROM vals`).Scan(&got)
	if err != nil || got != `{"a":1,"b":3}` {
		t.Fatalf("got %s, err %v", got, err)
	}
	// The legacy alias's default is tested via compiler lowering; the runtime
	// must honor the supplied cap even when inputs contain thousands of keys.
	properties := map[string]int{}
	for i := 0; i < 1500; i++ {
		properties[strings.Repeat("k", i+1)] = i
	}
	encoded, _ := json.Marshal(properties)
	err = db.QueryRow(`SELECT kql_make_bag(?,128)`, string(encoded)).Scan(&got)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err = json.Unmarshal([]byte(got), &decoded); err != nil || len(decoded) != 128 {
		t.Fatalf("len=%d err=%v", len(decoded), err)
	}
	// Exceeding the memory budget must fail, not silently drop properties.
	payload := strings.Repeat("x", maxBagBytes/2)
	err = db.QueryRow(`WITH vals(x) AS (VALUES (json_object('a',?)),(json_object('b',?))) SELECT kql_make_bag(x,1048576) FROM vals`, payload, payload).Scan(&got)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected resource error, got %v", err)
	}
}
