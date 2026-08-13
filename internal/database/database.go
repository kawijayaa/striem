package database

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db             *sql.DB
	challengeMu    sync.RWMutex
	challenge      ChallengeDefinition
	eventStorageMu sync.RWMutex
	indexedPaths   []string
	fullTextIndex  bool
	fullTextReady  bool
	fullTextDirty  bool
	storageSet     bool
}

type Dataset struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Table         string    `json:"table"`
	Signature     string    `json:"signature"`
	Source        string    `json:"source"`
	TimestampPath string    `json:"timestampPath"`
	EventCount    int64     `json:"eventCount"`
	CreatedAt     time.Time `json:"createdAt"`
}

type Field struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type FieldGroup struct {
	Table  string  `json:"table"`
	Fields []Field `json:"fields"`
}

func Open(path string) (*Store, error) {
	db, err := sql.Open(sqliteDriverName, path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	readerConnections := max(2, min(runtime.GOMAXPROCS(0), 4))
	if path == ":memory:" {
		readerConnections = 1
	}
	db.SetMaxOpenConns(readerConnections)
	db.SetMaxIdleConns(readerConnections)

	store := &Store{db: db}
	if err := store.initialize(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) initialize(ctx context.Context) error {
	const schema = `
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS datasets (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    table_name TEXT NOT NULL DEFAULT '',
    input_signature TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL,
    timestamp_path TEXT NOT NULL,
    event_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY,
    dataset_id INTEGER NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    time_generated TEXT NOT NULL,
    source TEXT NOT NULL,
    raw_data TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS dataset_fields (
    dataset_id INTEGER NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    type TEXT NOT NULL,
    PRIMARY KEY(dataset_id, path, type)
);

CREATE TABLE IF NOT EXISTS workspace_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS investigation_progress (
    question_id TEXT PRIMARY KEY,
    revision INTEGER NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_attempt_at TEXT,
    solved_at TEXT,
    answer TEXT
);

`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	if err := s.removeNormalizedEventColumns(ctx); err != nil {
		return err
	}
	datasetColumns := []struct {
		name        string
		definition  string
		description string
	}{
		{name: "table_name", definition: "TEXT NOT NULL DEFAULT ''", description: "dataset table name"},
		{name: "input_signature", definition: "TEXT NOT NULL DEFAULT ''", description: "dataset input signature"},
	}
	for _, column := range datasetColumns {
		if err := s.ensureColumn(ctx, "datasets", column.name, column.definition, column.description); err != nil {
			return err
		}
	}
	if err := s.ensureColumn(ctx, "investigation_progress", "answer", "TEXT", "question answer"); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DROP INDEX IF EXISTS idx_datasets_table_name;
CREATE INDEX idx_datasets_table_name ON datasets(table_name) WHERE table_name <> '';`); err != nil {
		return fmt.Errorf("initialize dataset indexes: %w", err)
	}
	if err := s.createOrdinaryEventIndexes(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Store) removeNormalizedEventColumns(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info(events)")
	if err != nil {
		return fmt.Errorf("inspect event schema: %w", err)
	}
	existing := make(map[string]struct{})
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("inspect event column: %w", err)
		}
		existing[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("inspect event schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("inspect event schema: %w", err)
	}
	legacy := []string{"event_type", "host", "username", "message"}
	found := false
	for _, column := range legacy {
		if _, ok := existing[column]; ok {
			found = true
		}
	}
	if !found {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate event schema: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
DROP INDEX IF EXISTS idx_events_type_time;
DROP INDEX IF EXISTS idx_events_host_time;
DROP INDEX IF EXISTS idx_events_host_dataset;
DROP INDEX IF EXISTS idx_events_user_time;`); err != nil {
		return fmt.Errorf("drop normalized event indexes: %w", err)
	}
	for _, column := range legacy {
		if _, ok := existing[column]; !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, "ALTER TABLE events DROP COLUMN "+column); err != nil {
			return fmt.Errorf("drop normalized event column %s: %w", column, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event schema migration: %w", err)
	}
	return nil
}

func (s *Store) ensureColumn(
	ctx context.Context,
	table string,
	column string,
	definition string,
	description string,
) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return fmt.Errorf("inspect %s schema: %w", table, err)
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("inspect %s column: %w", table, err)
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect %s schema: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("inspect %s schema: %w", table, err)
	}
	if found {
		return nil
	}
	statement := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("add %s: %w", description, err)
	}
	return nil
}

func (s *Store) DropEventIndexes(ctx context.Context) error {
	statements := []string{`
DROP INDEX IF EXISTS idx_events_time;
DROP INDEX IF EXISTS idx_events_source_time;
DROP INDEX IF EXISTS idx_events_type_time;
DROP INDEX IF EXISTS idx_events_host_time;
DROP INDEX IF EXISTS idx_events_host_dataset;
DROP INDEX IF EXISTS idx_events_user_time;
DROP INDEX IF EXISTS idx_events_dataset_time;`}
	expressionIndexes, err := s.eventExpressionIndexes(ctx)
	if err != nil {
		return err
	}
	for name := range expressionIndexes {
		statements = append(statements, "DROP INDEX "+name)
	}
	if _, err := s.db.ExecContext(ctx, strings.Join(statements, ";")); err != nil {
		return fmt.Errorf("drop event indexes: %w", err)
	}
	return nil
}

func (s *Store) CreateEventIndexes(ctx context.Context) error {
	if err := s.createOrdinaryEventIndexes(ctx); err != nil {
		return err
	}

	s.eventStorageMu.RLock()
	paths := append([]string(nil), s.indexedPaths...)
	configured := s.storageSet
	s.eventStorageMu.RUnlock()
	if !configured {
		return nil
	}
	desired := make(map[string]string, len(paths))
	for index, path := range paths {
		name := "idx_events_json_" + strconv.Itoa(index)
		desired[name] = eventExpressionIndexSQL(name, path)
	}
	existing, err := s.eventExpressionIndexes(ctx)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reconcile event expression indexes: %w", err)
	}
	defer tx.Rollback()
	for name, sqlText := range existing {
		if desired[name] == sqlText {
			delete(desired, name)
			continue
		}
		if _, err := tx.ExecContext(ctx, "DROP INDEX "+name); err != nil {
			return fmt.Errorf("drop stale event expression index %s: %w", name, err)
		}
	}
	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := tx.ExecContext(ctx, desired[name]); err != nil {
			return fmt.Errorf("create event expression index %s: %w", name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event expression indexes: %w", err)
	}
	return nil
}

func (s *Store) createOrdinaryEventIndexes(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS idx_events_time ON events(time_generated);
CREATE INDEX IF NOT EXISTS idx_events_source_time ON events(source, time_generated);
CREATE INDEX IF NOT EXISTS idx_events_dataset_time ON events(dataset_id, time_generated);`); err != nil {
		return fmt.Errorf("create event indexes: %w", err)
	}
	return nil
}

func (s *Store) ConfigureEventStorage(ctx context.Context, indexedPaths []string, fullTextIndex bool) error {
	unique := make(map[string]struct{}, len(indexedPaths))
	paths := make([]string, 0, len(indexedPaths))
	for _, path := range indexedPaths {
		if err := ValidateIndexedPath(path); err != nil {
			return err
		}
		if _, found := unique[path]; found {
			continue
		}
		unique[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if fullTextIndex {
		var supported int
		if err := s.db.QueryRowContext(ctx, `SELECT sqlite_compileoption_used('ENABLE_FTS5')`).Scan(&supported); err != nil {
			return fmt.Errorf("check SQLite FTS5 support: %w", err)
		}
		if supported == 0 {
			return fmt.Errorf("fullTextIndex requires a binary built with -tags sqlite_fts5")
		}
	}
	s.eventStorageMu.Lock()
	s.indexedPaths = paths
	s.fullTextIndex = fullTextIndex
	s.fullTextReady = false
	s.fullTextDirty = false
	s.storageSet = true
	s.eventStorageMu.Unlock()
	return nil
}

func ValidateIndexedPath(path string) error {
	if path == "" {
		return fmt.Errorf("indexed path cannot be empty")
	}
	for _, segment := range strings.Split(path, ".") {
		if segment == "" {
			return fmt.Errorf("indexed path %q contains an empty segment", path)
		}
		for index, character := range segment {
			if character != '_' && !unicode.IsLetter(character) && (index == 0 || !unicode.IsDigit(character)) {
				return fmt.Errorf("indexed path %q segment %q is not a KQL identifier", path, segment)
			}
		}
	}
	return nil
}

func (s *Store) FullTextIndexEnabled() bool {
	s.eventStorageMu.RLock()
	defer s.eventStorageMu.RUnlock()
	return s.fullTextIndex && s.fullTextReady
}

func (s *Store) MarkFullTextIndexDirty() {
	s.eventStorageMu.Lock()
	defer s.eventStorageMu.Unlock()
	if s.fullTextIndex {
		s.fullTextReady = false
		s.fullTextDirty = true
	}
}

func (s *Store) fullTextIndexConfigured() bool {
	s.eventStorageMu.RLock()
	defer s.eventStorageMu.RUnlock()
	return s.fullTextIndex
}

func (s *Store) DropFullTextIndex(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS events_fts`); err != nil {
		return fmt.Errorf("drop event full-text index: %w", err)
	}
	s.eventStorageMu.Lock()
	s.fullTextReady = false
	s.eventStorageMu.Unlock()
	return nil
}

func (s *Store) SyncFullTextIndex(ctx context.Context, rebuild bool) error {
	if !s.fullTextIndexConfigured() {
		return s.DropFullTextIndex(ctx)
	}
	s.eventStorageMu.RLock()
	ready, dirty := s.fullTextReady, s.fullTextDirty
	s.eventStorageMu.RUnlock()
	if !rebuild && ready {
		return nil
	}
	if dirty {
		rebuild = true
	}
	if !rebuild {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'events_fts')`).Scan(&exists); err != nil {
			return fmt.Errorf("inspect event full-text index: %w", err)
		}
		if exists != 0 {
			s.eventStorageMu.Lock()
			s.fullTextReady = true
			s.eventStorageMu.Unlock()
			return nil
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rebuild event full-text index: %w", err)
	}
	defer tx.Rollback()
	if rebuild {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS events_fts`); err != nil {
			return fmt.Errorf("drop event full-text index: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE VIRTUAL TABLE events_fts USING fts5(body, content='', tokenize='trigram')`); err != nil {
		return fmt.Errorf("create event full-text index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO events_fts(rowid, body)
SELECT id,
       coalesce(raw_data, '') || char(10) ||
       coalesce(time_generated, '') || char(10) ||
       coalesce(source, '')
FROM events`); err != nil {
		return fmt.Errorf("populate event full-text index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event full-text index: %w", err)
	}
	s.eventStorageMu.Lock()
	s.fullTextReady = true
	s.fullTextDirty = false
	s.eventStorageMu.Unlock()
	return nil
}

func (s *Store) eventExpressionIndexes(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT name, sql
FROM sqlite_master
WHERE type = 'index' AND name GLOB 'idx_events_json_[0-9]*'`)
	if err != nil {
		return nil, fmt.Errorf("inspect event expression indexes: %w", err)
	}
	defer rows.Close()
	indexes := make(map[string]string)
	for rows.Next() {
		var name, sqlText string
		if err := rows.Scan(&name, &sqlText); err != nil {
			return nil, fmt.Errorf("scan event expression index: %w", err)
		}
		if strings.TrimPrefix(name, "idx_events_json_") == "" {
			continue
		}
		if _, err := strconv.Atoi(strings.TrimPrefix(name, "idx_events_json_")); err != nil {
			continue
		}
		indexes[name] = sqlText
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect event expression indexes: %w", err)
	}
	return indexes, nil
}

func eventExpressionIndexSQL(name, path string) string {
	var jsonPath strings.Builder
	jsonPath.WriteByte('$')
	for _, segment := range strings.Split(path, ".") {
		jsonPath.WriteString(`."`)
		jsonPath.WriteString(segment)
		jsonPath.WriteByte('"')
	}
	return "CREATE INDEX " + name + " ON events(json_extract(raw_data, '" + jsonPath.String() + "'), dataset_id)"
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SetChallengeName(ctx context.Context, name string) error {
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO workspace_metadata(key, value) VALUES ('challenge_name', ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, name); err != nil {
		return fmt.Errorf("set challenge name: %w", err)
	}
	return nil
}

func (s *Store) ChallengeName(ctx context.Context) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM workspace_metadata WHERE key = 'challenge_name'").Scan(&name)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get challenge name: %w", err)
	}
	return name, nil
}

func (s *Store) ListDatasets(ctx context.Context) ([]Dataset, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, table_name, input_signature, source, timestamp_path, event_count, created_at
FROM datasets
ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list datasets: %w", err)
	}
	defer rows.Close()

	datasets := make([]Dataset, 0)
	for rows.Next() {
		var dataset Dataset
		var createdAt string
		if err := rows.Scan(&dataset.ID, &dataset.Name, &dataset.Table, &dataset.Signature, &dataset.Source, &dataset.TimestampPath, &dataset.EventCount, &createdAt); err != nil {
			return nil, fmt.Errorf("scan dataset: %w", err)
		}
		dataset.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		datasets = append(datasets, dataset)
	}
	return datasets, rows.Err()
}

func (s *Store) DeleteDataset(ctx context.Context, id int64) (bool, error) {
	result, err := s.db.ExecContext(ctx, "DELETE FROM datasets WHERE id = ?", id)
	if err != nil {
		return false, fmt.Errorf("delete dataset: %w", err)
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (s *Store) DeleteDatasetsExcept(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("at least one retained dataset is required")
	}
	placeholders := make([]string, len(names))
	arguments := make([]any, len(names))
	for index, name := range names {
		placeholders[index] = "?"
		arguments[index] = name
	}
	query := "DELETE FROM datasets WHERE name NOT IN (" + strings.Join(placeholders, ",") + ")"
	if _, err := s.db.ExecContext(ctx, query, arguments...); err != nil {
		return fmt.Errorf("remove unconfigured datasets: %w", err)
	}
	return nil
}

func (s *Store) ListFields(ctx context.Context) ([]Field, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT path,
       CASE WHEN COUNT(DISTINCT type) = 1 THEN MIN(type) ELSE 'mixed' END
FROM dataset_fields
GROUP BY path
ORDER BY path`)
	if err != nil {
		return nil, fmt.Errorf("list fields: %w", err)
	}
	defer rows.Close()
	fields := make([]Field, 0)
	for rows.Next() {
		var field Field
		if err := rows.Scan(&field.Path, &field.Type); err != nil {
			return nil, fmt.Errorf("scan field: %w", err)
		}
		fields = append(fields, field)
	}
	return fields, rows.Err()
}

func (s *Store) ListFieldGroups(ctx context.Context) ([]FieldGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT d.table_name, f.path,
       CASE WHEN COUNT(DISTINCT f.type) = 1 THEN MIN(f.type) ELSE 'mixed' END
FROM dataset_fields AS f
JOIN datasets AS d ON d.id = f.dataset_id
WHERE d.table_name <> ''
GROUP BY d.table_name, f.path
ORDER BY d.table_name, f.path`)
	if err != nil {
		return nil, fmt.Errorf("list field groups: %w", err)
	}
	defer rows.Close()
	groups := make([]FieldGroup, 0)
	for rows.Next() {
		var table string
		var field Field
		if err := rows.Scan(&table, &field.Path, &field.Type); err != nil {
			return nil, fmt.Errorf("scan field group: %w", err)
		}
		if len(groups) == 0 || groups[len(groups)-1].Table != table {
			groups = append(groups, FieldGroup{Table: table, Fields: make([]Field, 0)})
		}
		groups[len(groups)-1].Fields = append(groups[len(groups)-1].Fields, field)
	}
	return groups, rows.Err()
}
