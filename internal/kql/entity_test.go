package kql

import (
	"fmt"
	"testing"
	"time"
)

// Entity functions must not reach Striem's storage tables (datasets, events,
// workspace metadata, investigation progress) or SQLite internals.
func TestEntityFunctionsCannotReadStorageTables(t *testing.T) {
	for _, query := range []string{
		`table('sqlite_master')`, `table("investigation_progress")`, `table("workspace_metadata") | take 1`,
		`table("datasets")`, `materialized_view("sqlite_master")`, `external_table("dataset_fields")`,
		`union table("sqlite_master"), LeftEvents`,
	} {
		if compiled, err := Compile(query, time.Now(), featureCatalog()); err == nil {
			t.Fatalf("%s compiled to %s", query, compiled.SQL)
		}
	}
	db := featureDatabase(t)
	if got := fmt.Sprint(featureRows(t, db, `table("LeftEvents") | count`)); got != `[[2]]` {
		t.Fatalf("catalog table through table(): %s", got)
	}
}
