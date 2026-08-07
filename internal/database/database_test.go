package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"strings"
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
	if err := store.DropEventIndexes(t.Context()); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := store.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name GLOB 'idx_events_*'`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("event indexes after drop = %d, want 0", remaining)
	}
}

func TestWorkspaceMetadataAndDatasetCatalogue(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "catalogue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	name, err := store.ChallengeName(t.Context())
	if err != nil || name != "" {
		t.Fatalf("initial challenge name = %q, err = %v", name, err)
	}
	for _, want := range []string{"First name", "Updated name"} {
		if err := store.SetChallengeName(t.Context(), want); err != nil {
			t.Fatal(err)
		}
		name, err = store.ChallengeName(t.Context())
		if err != nil || name != want {
			t.Fatalf("challenge name = %q, err = %v, want %q", name, err, want)
		}
	}

	if _, err := store.DB().ExecContext(t.Context(), `
INSERT INTO datasets(id, name, table_name, input_signature, source, timestamp_path, event_count, created_at)
VALUES (1, 'alpha', 'Alpha', 'a', 'one', 'ts', 1, '2026-08-07T00:00:00Z'),
       (2, 'beta', 'Beta', 'b', 'two', 'ts', 1, '2026-08-07T00:00:00Z'),
       (3, 'gamma', 'Gamma', 'c', 'three', 'ts', 1, '2026-08-07T00:00:00Z');
INSERT INTO dataset_fields(dataset_id, path, type)
VALUES (1, 'Shared', 'string'),
       (2, 'Shared', 'number'),
       (2, 'Unique', 'bool');
INSERT INTO events(dataset_id, time_generated, source, raw_data)
VALUES (3, '2026-08-07T00:00:00Z', 'three', '{}');`); err != nil {
		t.Fatal(err)
	}

	fields, err := store.ListFields(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || fields[0] != (Field{Path: "Shared", Type: "mixed"}) || fields[1] != (Field{Path: "Unique", Type: "bool"}) {
		t.Fatalf("fields = %#v", fields)
	}
	groups, err := store.ListFieldGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0].Table != "Alpha" || groups[1].Table != "Beta" || len(groups[1].Fields) != 2 {
		t.Fatalf("field groups = %#v", groups)
	}

	if deleted, err := store.DeleteDataset(t.Context(), 99); err != nil || deleted {
		t.Fatalf("delete missing dataset = %t, err = %v", deleted, err)
	}
	if deleted, err := store.DeleteDataset(t.Context(), 3); err != nil || !deleted {
		t.Fatalf("delete dataset = %t, err = %v", deleted, err)
	}
	var events int
	if err := store.DB().QueryRowContext(t.Context(), "SELECT count(*) FROM events WHERE dataset_id = 3").Scan(&events); err != nil || events != 0 {
		t.Fatalf("events after cascade = %d, err = %v", events, err)
	}
	if err := store.DeleteDatasetsExcept(t.Context(), nil); err == nil {
		t.Fatal("DeleteDatasetsExcept accepted an empty retention list")
	}
	if err := store.DeleteDatasetsExcept(t.Context(), []string{"beta"}); err != nil {
		t.Fatal(err)
	}
	datasets, err := store.ListDatasets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(datasets) != 1 || datasets[0].Name != "beta" {
		t.Fatalf("retained datasets = %#v", datasets)
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

func TestOpenRemovesNormalizedEventColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-events.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE datasets (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    table_name TEXT NOT NULL DEFAULT '',
    input_signature TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL,
    timestamp_path TEXT NOT NULL,
    event_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE TABLE events (
    id INTEGER PRIMARY KEY,
    dataset_id INTEGER NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    time_generated TEXT NOT NULL,
    source TEXT NOT NULL,
    event_type TEXT,
    host TEXT,
    username TEXT,
    message TEXT,
    raw_data TEXT NOT NULL
);
CREATE INDEX idx_events_type_time ON events(event_type, time_generated);
CREATE INDEX idx_events_host_time ON events(host, time_generated);
CREATE INDEX idx_events_host_dataset ON events(host, dataset_id);
CREATE INDEX idx_events_user_time ON events(username, time_generated);
INSERT INTO datasets(id, name, table_name, source, timestamp_path, created_at)
VALUES (1, 'legacy', 'Legacy', 'source', 'ts', '2026-08-07T00:00:00Z');
INSERT INTO events(dataset_id, time_generated, source, event_type, host, username, message, raw_data)
VALUES (1, '2026-08-07T00:00:00Z', 'source', 'login', 'pc-1', 'alice', 'signed in', '{"event_type":"login","host":"pc-1","user":"alice","message":"signed in"}');`); err != nil {
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
	rows, err := store.DB().Query("PRAGMA table_info(events)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	if got := strings.Join(columns, ","); got != "id,dataset_id,time_generated,source,raw_data" {
		t.Fatalf("event columns = %s", got)
	}
	var raw string
	if err := store.DB().QueryRow("SELECT raw_data FROM events WHERE id = 1").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"host":"pc-1"`) {
		t.Fatalf("migrated raw data = %s", raw)
	}
}
