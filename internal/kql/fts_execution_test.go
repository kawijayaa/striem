//go:build sqlite_fts5

package kql

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kawijayaa/striem/internal/database"
)

func TestLeadingSearchFullTextPrefilterExecutesAndRechecks(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ConfigureEventStorage(t.Context(), nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`
INSERT INTO datasets(id, name, table_name, source, timestamp_path, created_at)
VALUES (1, 'suricata', 'Suricata', 'suricata', 'ts', '2026-07-29T00:00:00Z');
INSERT INTO events(id, dataset_id, time_generated, source, host, message, raw_data) VALUES
    (1, 1, '2026-07-29T00:00:00Z', 'suricata', 'valid', 'PowerShell from 10.10.1.9', '{}'),
    (2, 1, '2026-07-29T00:00:00Z', 'suricata', 'recheck', 'PowerShellExtra', '{}'),
    (3, 1, '2026-07-29T00:00:00Z', 'suricata', 'other', 'benign', '{}')`); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncFullTextIndex(t.Context(), true); err != nil {
		t.Fatal(err)
	}

	for _, term := range []string{"powershell", "10.10.1.9"} {
		compiled, err := Compile(`Suricata | search "`+term+`" | project Host`, time.Now(), CompileConfig{
			Tables: TableCatalog{"Suricata": 1}, FullTextIndex: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		var host string
		if err := store.DB().QueryRow(compiled.SQL, compiled.Args...).Scan(&host); err != nil {
			t.Fatalf("search %q: %v\nSQL: %s\nArgs: %#v", term, err, compiled.SQL, compiled.Args)
		}
		if host != "valid" {
			t.Fatalf("search %q host = %q, want valid", term, host)
		}
	}
}
