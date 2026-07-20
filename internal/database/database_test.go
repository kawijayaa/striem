package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenMigratesDatasetTableNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE datasets (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    source TEXT NOT NULL,
    timestamp_path TEXT NOT NULL,
    event_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
INSERT INTO datasets(name, source, timestamp_path, created_at)
VALUES ('legacy', 'legacy', 'ts', '2024-01-01T00:00:00Z');`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	datasets, err := store.ListDatasets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(datasets) != 1 || datasets[0].Table != "" {
		t.Fatalf("migrated datasets = %#v", datasets)
	}
	if _, err := store.DB().Exec("UPDATE datasets SET table_name = 'Legacy' WHERE name = 'legacy'"); err != nil {
		t.Fatalf("update migrated table name: %v", err)
	}
	if _, err := store.DB().Exec("UPDATE datasets SET input_signature = 'signature' WHERE name = 'legacy'"); err != nil {
		t.Fatalf("update migrated input signature: %v", err)
	}
}
