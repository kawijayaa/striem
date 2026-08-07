//go:build sqlite_fts5

package database

import (
	"path/filepath"
	"testing"
)

func TestFullTextIndexRebuildAndDisable(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "fts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ConfigureEventStorage(t.Context(), nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`
INSERT INTO datasets(id, name, source, timestamp_path, created_at)
VALUES (1, 'first', 'suricata', 'ts', '2026-07-29T00:00:00Z');
INSERT INTO events(id, dataset_id, time_generated, source, raw_data)
VALUES (11, 1, '2026-07-29T00:00:00Z', 'suricata', '{"host":"sensor-1","message":"network alert","src_ip":"10.10.1.9"}')`); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncFullTextIndex(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	assertFTSRowIDs(t, store, `"10.10.1.9"`, []int64{11})
	assertFTSRowIDs(t, store, `"sensor-1"`, []int64{11})

	if _, err := store.DB().Exec(`DELETE FROM datasets WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncFullTextIndex(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	assertFTSRowIDs(t, store, `"10.10.1.9"`, nil)

	if err := store.ConfigureEventStorage(t.Context(), nil, false); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncFullTextIndex(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	var exists int
	if err := store.DB().QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE name = 'events_fts')`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != 0 {
		t.Fatal("events_fts remains after full-text indexing was disabled")
	}
}

func assertFTSRowIDs(t *testing.T, store *Store, match string, want []int64) {
	t.Helper()
	rows, err := store.DB().Query(`SELECT rowid FROM events_fts WHERE events_fts MATCH ? ORDER BY rowid`, match)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []int64
	for rows.Next() {
		var rowID int64
		if err := rows.Scan(&rowID); err != nil {
			t.Fatal(err)
		}
		got = append(got, rowID)
	}
	if len(got) != len(want) {
		t.Fatalf("FTS rowids = %v, want %v", got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("FTS rowids = %v, want %v", got, want)
		}
	}
}
