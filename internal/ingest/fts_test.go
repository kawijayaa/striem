//go:build sqlite_fts5

package ingest

import (
	"strings"
	"testing"
)

func TestImportMarksFullTextIndexDirtyUntilRebuilt(t *testing.T) {
	store := openTestStore(t)
	if err := store.ConfigureEventStorage(t.Context(), nil, true); err != nil {
		t.Fatal(err)
	}
	service := New(store)
	if _, err := service.Import(t.Context(), strings.NewReader(`{"ts":"2026-07-29T00:00:00Z","message":"first"}`), false, Mapping{
		Name: "first", Table: "First", Source: "test", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncFullTextIndex(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if !store.FullTextIndexEnabled() {
		t.Fatal("full-text prefilter is disabled after rebuild")
	}
	if _, err := service.Import(t.Context(), strings.NewReader(`{"ts":"2026-07-29T00:00:00Z","message":"second"}`), false, Mapping{
		Name: "second", Table: "Second", Source: "test", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}
	if store.FullTextIndexEnabled() {
		t.Fatal("stale full-text prefilter remains enabled after import")
	}
	if err := store.SyncFullTextIndex(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	if !store.FullTextIndexEnabled() {
		t.Fatal("full-text prefilter is disabled after dirty rebuild")
	}
	var matches int
	if err := store.DB().QueryRow(`SELECT count(*) FROM events_fts WHERE events_fts MATCH 'second'`).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	if matches != 1 {
		t.Fatalf("full-text matches = %d, want 1", matches)
	}
}
