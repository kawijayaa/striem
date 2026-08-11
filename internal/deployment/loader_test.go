package deployment

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kawijayaa/striem/internal/database"
	"github.com/kawijayaa/striem/internal/kql"
)

func TestLoadUsesRelativePathsAndReplacesDataset(t *testing.T) {
	directory := t.TempDir()
	eventsPath := filepath.Join(directory, "events.ndjson")
	manifestPath := filepath.Join(directory, "datasets.yaml")
	if err := os.WriteFile(eventsPath, []byte(`{"ts":"2024-01-01T00:00:00Z","host":"pc-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `challengeName: Operation Northstar
flag: flag{northstar}
submissionCooldown: 2s
questions:
  - id: source-ip
    title: Identify the source
    prompt: Which source IP generated the alert?
    acceptedAnswers:
      - 192.0.2.1
datasets:
  - name: challenge
    table: Challenge
    path: events.ndjson
    source: fixture
    timestampPath: ts
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	challengeName, err := ReadChallengeName(manifestPath)
	if err != nil || challengeName != "Operation Northstar" {
		t.Fatalf("pre-ingestion challenge name = %q, %v", challengeName, err)
	}
	store, err := database.Open(filepath.Join(directory, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first, err := Load(t.Context(), store, manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO datasets(name,source,timestamp_path,event_count,created_at) VALUES('stale','old','ts',0,'2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	second, err := Load(t.Context(), store, manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if first[0].ID != second[0].ID {
		t.Fatalf("unchanged dataset was reimported: first ID %d, second ID %d", first[0].ID, second[0].ID)
	}
	var datasets, events int
	store.DB().QueryRow("SELECT COUNT(*) FROM datasets").Scan(&datasets)
	store.DB().QueryRow("SELECT COUNT(*) FROM events").Scan(&events)
	if datasets != 1 || events != 1 {
		t.Fatalf("reload produced %d datasets and %d events, want one of each", datasets, events)
	}
	challengeName, err = store.ChallengeName(t.Context())
	if err != nil || challengeName != "Operation Northstar" {
		t.Fatalf("challenge name = %q, %v", challengeName, err)
	}
	challenge, err := store.ChallengeState(t.Context())
	if err != nil || challenge.Total != 1 || challenge.Questions[0].ID != "source-ip" {
		t.Fatalf("challenge state = %#v, %v", challenge, err)
	}
}

func TestLoadReimportsChangedDataset(t *testing.T) {
	directory := t.TempDir()
	eventsPath := filepath.Join(directory, "events.ndjson")
	manifestPath := filepath.Join(directory, "datasets.yaml")
	if err := os.WriteFile(eventsPath, []byte(`{"ts":"2024-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `datasets:
  - name: challenge
    table: Challenge
    path: events.ndjson
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
	first, err := Load(t.Context(), store, manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(eventsPath, []byte("{\"ts\":\"2024-01-01T00:00:00Z\"}\n{\"ts\":\"2024-01-01T00:01:00Z\"}"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Load(t.Context(), store, manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if first[0].Signature == second[0].Signature || second[0].EventCount != 2 {
		t.Fatalf("changed dataset was not replaced: first=%#v second=%#v", first[0], second[0])
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
			manifest := `datasets:
  - name: csv
    table: CSV
    path: PLACEHOLDER
    source: fixture
    timestampPath: ts
`
			manifest = strings.Replace(manifest, "PLACEHOLDER", fileName, 1)
			manifestPath := filepath.Join(directory, "datasets.yaml")
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
	manifestPath := filepath.Join(directory, "datasets.yaml")
	manifest := `datasets:
  - name: bad
    table: Bad
    path: events.tsv
    format: tsv
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
	if _, err := Load(t.Context(), store, manifestPath); err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("Load() error = %v, want unsupported format", err)
	}
	var indexes int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_events_time'`).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != 1 {
		t.Fatal("event indexes were not restored after failed deployment")
	}
}

func TestDatasetFormatDetectsEVTX(t *testing.T) {
	for _, test := range []struct {
		path       string
		configured string
		want       string
	}{
		{path: "security.evtx", want: "evtx"},
		{path: "security.EVTX", configured: "auto", want: "evtx"},
		{path: "security.evtx.gz", want: "evtx"},
		{path: "security.EVTX.GZ", want: "evtx"},
		{path: "security.bin", configured: "EVTX", want: "evtx"},
	} {
		t.Run(test.path+test.configured, func(t *testing.T) {
			got, err := datasetFormat(test.path, test.configured)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("datasetFormat(%q, %q) = %q, want %q", test.path, test.configured, got, test.want)
			}
		})
	}
}

func TestLoadRejectsDuplicateTables(t *testing.T) {
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "datasets.yaml")
	if err := os.WriteFile(filepath.Join(directory, "one.json"), []byte(`{"ts":"2024-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `datasets:
  - name: one
    table: Shared
    path: one.json
    source: one
    timestampPath: ts
  - name: two
    table: Shared
    path: two.json
    source: two
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
	if _, err := Load(t.Context(), store, manifestPath); err == nil || !strings.Contains(err.Error(), "configured more than once") {
		t.Fatalf("Load() error = %v, want duplicate table error", err)
	}
}

func TestLoadRejectsInvalidQuestions(t *testing.T) {
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "datasets.yaml")
	manifest := `questions:
  - id: source-ip
    title: Source
    prompt: Which source?
    acceptedAnswers: [192.0.2.1]
datasets:
  - name: one
    table: One
    path: one.json
    source: one
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
	if _, err := Load(t.Context(), store, manifestPath); err == nil || !strings.Contains(err.Error(), "flag is required") {
		t.Fatalf("Load() error = %v, want required flag", err)
	}
}

func TestValidateChallengeAllowsNormalWorkspaceWithoutQuestions(t *testing.T) {
	challenge, err := validateChallenge(Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	if challenge.Flag != "" || len(challenge.Questions) != 0 {
		t.Fatalf("challenge = %#v, want an empty optional challenge", challenge)
	}
}

func TestLoadUnionsIndexedPathsAndQueryUsesExpressionIndex(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "one.json"), []byte(`{"ts":"2024-01-01T00:00:00Z","src_ip":"198.51.100.77","alert":{"signature_id":42}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "two.json"), []byte(`{"ts":"2024-01-01T00:00:00Z","dest_ip":"192.0.2.4"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `datasets:
  - name: one
    table: One
    path: one.json
    source: fixture
    timestampPath: ts
    indexedPaths: [src_ip, alert.signature_id]
  - name: two
    table: Two
    path: two.json
    source: fixture
    timestampPath: ts
    indexedPaths: [dest_ip, src_ip]
`
	manifestPath := filepath.Join(directory, "datasets.yaml")
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
	var indexCount int
	if err := store.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name GLOB 'idx_events_json_*'`).Scan(&indexCount); err != nil {
		t.Fatal(err)
	}
	if indexCount != 3 {
		t.Fatalf("expression index count = %d, want 3", indexCount)
	}

	compiled, err := kql.Compile(`One | where src_ip == "198.51.100.77" | project RawData`, time.Now(), kql.TableCatalog{"One": {ID: loaded[0].ID, Fields: []kql.Field{{Name: "src_ip", Type: "string"}}}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.DB().QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+compiled.SQL, compiled.Args...)
	if err != nil {
		t.Fatalf("explain query: %v\nSQL: %s", err, compiled.SQL)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "USING INDEX idx_events_json_2") {
		t.Fatalf("query plan does not use src_ip expression index:\n%s\nSQL: %s", joined, compiled.SQL)
	}
	if strings.Contains(joined, "SCAN events") {
		t.Fatalf("query plan scans events:\n%s", joined)
	}
}

func TestLoadRejectsInvalidIndexedPathBeforeImport(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "events.json"), []byte(`{"ts":"2024-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(directory, "datasets.yaml")
	manifest := `datasets:
  - name: invalid
    table: Invalid
    path: events.json
    source: fixture
    timestampPath: ts
    indexedPaths: [alert.signature-id]
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := database.Open(filepath.Join(directory, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := Load(t.Context(), store, manifestPath); err == nil || !strings.Contains(err.Error(), "not a KQL identifier") {
		t.Fatalf("Load() error = %v, want indexed path validation", err)
	}
	var datasets int
	if err := store.DB().QueryRow(`SELECT count(*) FROM datasets`).Scan(&datasets); err != nil {
		t.Fatal(err)
	}
	if datasets != 0 {
		t.Fatalf("datasets imported before validation = %d", datasets)
	}
}
