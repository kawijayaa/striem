package database

import (
	"encoding/json"
	"testing"
)

func TestCollectionAggregatesAreBounded(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var encoded string
	err = store.DB().QueryRow(`WITH RECURSIVE values_(value) AS (
		SELECT 1 UNION ALL SELECT value + 1 FROM values_ WHERE value < 1200
	) SELECT kql_make_list(value, 0) FROM values_`).Scan(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	var values []any
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != maxCollectionValues {
		t.Fatalf("values = %d, want %d", len(values), maxCollectionValues)
	}
}
