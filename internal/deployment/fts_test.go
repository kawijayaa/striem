//go:build sqlite_fts5

package deployment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kawijayaa/striem/internal/database"
)

func TestFullTextIndexRecoversAfterFailedReplacement(t *testing.T) {
	directory := t.TempDir()
	onePath := filepath.Join(directory, "one.json")
	twoPath := filepath.Join(directory, "two.json")
	manifestPath := filepath.Join(directory, "datasets.yaml")
	if err := os.WriteFile(onePath, []byte(`{"ts":"2024-01-01T00:00:00Z","value":"old-first"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(twoPath, []byte(`{"ts":"2024-01-01T00:00:00Z","value":"old-second"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `fullTextIndex: true
datasets:
  - name: one
    table: One
    path: one.json
    source: fixture
    timestampPath: ts
  - name: two
    table: Two
    path: two.json
    source: fixture
    timestampPath: ts
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := database.Open(filepath.Join(directory, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := Load(t.Context(), store, manifestPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(t.Context(), store, manifestPath); err != nil {
		t.Fatalf("unchanged startup: %v", err)
	}
	assertFTSCount(t, store, `"old-first"`, 1)

	if err := os.WriteFile(onePath, []byte(`{"ts":"2024-01-01T00:00:00Z","value":"new-first-longer"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(twoPath, []byte(`not valid json and a different size`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(t.Context(), store, manifestPath); err == nil {
		t.Fatal("replacement with an invalid second dataset succeeded")
	}
	assertFTSCount(t, store, `"old-first"`, 0)
	assertFTSCount(t, store, `"new-first-longer"`, 1)
	assertFTSCount(t, store, `"old-second"`, 1)
}

func assertFTSCount(t *testing.T, store *database.Store, match string, want int) {
	t.Helper()
	var got int
	if err := store.DB().QueryRow(`SELECT count(*) FROM events_fts WHERE events_fts MATCH ?`, match).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("FTS count for %q = %d, want %d", match, got, want)
	}
}
