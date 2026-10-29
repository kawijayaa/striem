package database

import (
	"path/filepath"
	"strings"
	"testing"
)

// Even raw SQL on the user query pool cannot reach progress, metadata,
// schema internals, other database files or pragmas.
func TestQueryConnectionsDenySensitiveAccess(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, query := range []string{
		"SELECT answer FROM investigation_progress",
		"SELECT value FROM workspace_metadata",
		"SELECT sql FROM sqlite_master",
		"SELECT sql FROM sqlite_schema",
		"SELECT * FROM pragma_table_info('investigation_progress')",
		"SELECT name FROM pragma_function_list",
		"PRAGMA table_info(investigation_progress)",
		"ATTACH DATABASE ':memory:' AS other",
		"SELECT load_extension('x')",
	} {
		rows, err := store.QueryDB().QueryContext(t.Context(), query)
		if err == nil {
			rows.Close()
			t.Errorf("query pool ran %q", query)
		} else if !strings.Contains(err.Error(), "not authorized") && !strings.Contains(err.Error(), "prohibited") {
			t.Errorf("%q failed with %v, want an authorization error", query, err)
		}
	}
	var count int
	if err := store.QueryDB().QueryRowContext(t.Context(), "SELECT count(*) FROM events JOIN datasets ON datasets.id = events.dataset_id").Scan(&count); err != nil {
		t.Errorf("query pool cannot read events: %v", err)
	}
	// The ingestion and progress pool is not restricted.
	if err := store.DB().QueryRowContext(t.Context(), "SELECT count(*) FROM investigation_progress").Scan(&count); err != nil {
		t.Errorf("main pool cannot read progress: %v", err)
	}
}
