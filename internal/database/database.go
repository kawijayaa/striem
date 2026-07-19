package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Dataset struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Source        string    `json:"source"`
	TimestampPath string    `json:"timestampPath"`
	EventCount    int64     `json:"eventCount"`
	CreatedAt     time.Time `json:"createdAt"`
}

type Field struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// A single connection keeps SQLite's connection-local PRAGMAs consistent and
	// is sufficient for the intentionally small per-team deployment.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

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
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS datasets (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
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
    event_type TEXT,
    host TEXT,
    username TEXT,
    message TEXT,
    raw_data TEXT NOT NULL CHECK (json_valid(raw_data))
);

CREATE TABLE IF NOT EXISTS dataset_fields (
    dataset_id INTEGER NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    type TEXT NOT NULL,
    PRIMARY KEY(dataset_id, path, type)
);

CREATE INDEX IF NOT EXISTS idx_events_time ON events(time_generated);
CREATE INDEX IF NOT EXISTS idx_events_source_time ON events(source, time_generated);
CREATE INDEX IF NOT EXISTS idx_events_type_time ON events(event_type, time_generated);
CREATE INDEX IF NOT EXISTS idx_events_host_time ON events(host, time_generated);
CREATE INDEX IF NOT EXISTS idx_events_user_time ON events(username, time_generated);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	return nil
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) ListDatasets(ctx context.Context) ([]Dataset, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, source, timestamp_path, event_count, created_at
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
		if err := rows.Scan(&dataset.ID, &dataset.Name, &dataset.Source, &dataset.TimestampPath, &dataset.EventCount, &createdAt); err != nil {
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
