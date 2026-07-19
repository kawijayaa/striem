package deployment

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oka/striem/internal/database"
)

func TestLoadUsesRelativePathsAndReplacesDataset(t *testing.T) {
	directory := t.TempDir()
	eventsPath := filepath.Join(directory, "events.ndjson")
	manifestPath := filepath.Join(directory, "datasets.json")
	if err := os.WriteFile(eventsPath, []byte(`{"ts":"2024-01-01T00:00:00Z","host":"pc-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `{"datasets":[{"name":"challenge","path":"events.ndjson","source":"fixture","timestampPath":"ts","fieldPaths":{"Host":"host"}}]}`
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
	if _, err := store.DB().Exec(`INSERT INTO datasets(name,source,timestamp_path,event_count,created_at) VALUES('stale','old','ts',0,'2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(t.Context(), store, manifestPath); err != nil {
		t.Fatal(err)
	}
	var datasets, events int
	store.DB().QueryRow("SELECT COUNT(*) FROM datasets").Scan(&datasets)
	store.DB().QueryRow("SELECT COUNT(*) FROM events").Scan(&events)
	if datasets != 1 || events != 1 {
		t.Fatalf("reload produced %d datasets and %d events, want one of each", datasets, events)
	}
}

func TestLoadDetectsCSVAndGzip(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		name := "csv"
		if compressed {
			name = "csv gzip"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			fileName := "events.csv"
			content := []byte("ts,host\n2024-01-01T00:00:00Z,pc-1\n")
			if compressed {
				fileName += ".gz"
				var buffer bytes.Buffer
				writer := gzip.NewWriter(&buffer)
				if _, err := writer.Write(content); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				content = buffer.Bytes()
			}
			if err := os.WriteFile(filepath.Join(directory, fileName), content, 0o600); err != nil {
				t.Fatal(err)
			}
			manifest := `{"datasets":[{"name":"csv","path":"PLACEHOLDER","source":"fixture","timestampPath":"ts","fieldPaths":{"Host":"host"}}]}`
			manifest = strings.Replace(manifest, "PLACEHOLDER", fileName, 1)
			manifestPath := filepath.Join(directory, "datasets.json")
			if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := database.Open(filepath.Join(directory, "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			loaded, err := Load(t.Context(), store, manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded) != 1 || loaded[0].EventCount != 1 {
				t.Fatalf("loaded datasets = %#v", loaded)
			}
		})
	}
}

func TestLoadRejectsUnsupportedFormat(t *testing.T) {
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "datasets.json")
	manifest := `{"datasets":[{"name":"bad","path":"events.tsv","format":"tsv","source":"fixture","timestampPath":"ts"}]}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := database.Open(filepath.Join(directory, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := Load(t.Context(), store, manifestPath); err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("Load() error = %v, want unsupported format", err)
	}
}
