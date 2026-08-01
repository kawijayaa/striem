package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestEventExpressionIndexesAreDeterministicAndReconciled(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "indexes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ConfigureEventStorage(t.Context(), []string{"src_ip", "alert.signature_id", "src_ip"}, false); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEventIndexes(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertIndexSQL := func(name, want string) {
		t.Helper()
		var got string
		if err := store.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s SQL = %q, want %q", name, got, want)
		}
	}
	assertIndexSQL("idx_events_json_0", `CREATE INDEX idx_events_json_0 ON events(json_extract(raw_data, '$."alert"."signature_id"'), dataset_id)`)
	assertIndexSQL("idx_events_json_1", `CREATE INDEX idx_events_json_1 ON events(json_extract(raw_data, '$."src_ip"'), dataset_id)`)

	if err := store.ConfigureEventStorage(t.Context(), []string{"dest_ip"}, false); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEventIndexes(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertIndexSQL("idx_events_json_0", `CREATE INDEX idx_events_json_0 ON events(json_extract(raw_data, '$."dest_ip"'), dataset_id)`)
	var stale int
	if err := store.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_events_json_1'`).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Fatal("stale expression index was not dropped")
	}
}

func TestValidateIndexedPath(t *testing.T) {
	for _, path := range []string{"src_ip", "alert.signature_id", "Event.System.EventID"} {
		if err := ValidateIndexedPath(path); err != nil {
			t.Errorf("ValidateIndexedPath(%q) = %v", path, err)
		}
	}
	for _, path := range []string{"", ".src", "src.", "alert..id", "field-with-hyphen", "1field", `field"name`} {
		if err := ValidateIndexedPath(path); err == nil {
			t.Errorf("ValidateIndexedPath(%q) succeeded", path)
		}
	}
}

func TestOpenConfiguresConcurrentReaderConnections(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "pool.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	wantConnections := max(2, min(runtime.GOMAXPROCS(0), 4))
	if got := store.DB().Stats().MaxOpenConnections; got != wantConnections {
		t.Fatalf("maximum open connections = %d, want %d", got, wantConnections)
	}

	connections := make([]*sql.Conn, wantConnections)
	for index := range connections {
		connections[index], err = store.DB().Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer connections[index].Close()
	}
	if got := store.DB().Stats().OpenConnections; got != wantConnections {
		t.Fatalf("open connections = %d, want %d", got, wantConnections)
	}

	pragmas := []struct {
		name string
		want int64
	}{
		{name: "foreign_keys", want: 1},
		{name: "busy_timeout", want: 5000},
		{name: "synchronous", want: 1},
		{name: "cache_size", want: -131072},
		{name: "temp_store", want: 2},
		{name: "mmap_size", want: 268435456},
	}
	for index, connection := range connections {
		for _, pragma := range pragmas {
			var got int64
			if err := connection.QueryRowContext(t.Context(), "PRAGMA "+pragma.name).Scan(&got); err != nil {
				t.Fatalf("connection %d PRAGMA %s: %v", index, pragma.name, err)
			}
			if got != pragma.want {
				t.Errorf("connection %d PRAGMA %s = %d, want %d", index, pragma.name, got, pragma.want)
			}
		}
	}

	start := make(chan struct{})
	errors := make(chan error, len(connections))
	var wait sync.WaitGroup
	for _, connection := range connections {
		wait.Add(1)
		go func(connection *sql.Conn) {
			defer wait.Done()
			<-start
			var count int
			errors <- connection.QueryRowContext(context.Background(), "SELECT count(*) FROM datasets").Scan(&count)
		}(connection)
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent read: %v", err)
		}
	}
}

func TestOpenKeepsPrivateMemoryDatabaseOnOneConnection(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got := store.DB().Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("maximum open connections = %d, want 1", got)
	}
	if err := store.DB().PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestForeignKeysCascadeOnPooledConnection(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cascade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	writer, err := store.DB().Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.ExecContext(t.Context(), `
INSERT INTO datasets(id, name, source, timestamp_path, created_at)
VALUES (1, 'dataset', 'source', 'timestamp', '2026-07-29T00:00:00Z');
INSERT INTO events(dataset_id, time_generated, source, raw_data)
VALUES (1, '2026-07-29T00:00:00Z', 'source', '{}');
INSERT INTO dataset_fields(dataset_id, path, type)
VALUES (1, 'field', 'string');`); err != nil {
		t.Fatal(err)
	}

	deleter, err := store.DB().Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer deleter.Close()
	if _, err := deleter.ExecContext(t.Context(), "DELETE FROM datasets WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"events", "dataset_fields"} {
		var count int
		if err := deleter.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("%s rows after dataset deletion = %d, want 0", table, count)
		}
	}
}

func TestOpenMigratesDatasetTableNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite3", path)
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

func TestOpenMigratesQuestionAnswers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE investigation_progress (
    question_id TEXT PRIMARY KEY,
    revision INTEGER NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_attempt_at TEXT,
    solved_at TEXT
);`); err != nil {
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
	if _, err := store.DB().Exec(`
INSERT INTO investigation_progress(question_id, revision, answer)
VALUES ('question', 1, 'accepted')`); err != nil {
		t.Fatalf("write migrated question answer: %v", err)
	}
}
