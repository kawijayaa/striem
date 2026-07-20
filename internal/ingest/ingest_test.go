package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/oka/striem/internal/database"
)

func TestImportPreservesTimestampAndMapsFields(t *testing.T) {
	store := openTestStore(t)
	service := New(store)
	input := strings.NewReader(`
{"ts":"2024-01-02T03:04:05+02:00","kind":"process","host":{"name":"pc-1"},"message":"created"}
{"ts":"2024-01-02T03:05:05+02:00","kind":"network","host":{"name":"pc-1"},"message":"connected"}
`)
	result, err := service.Import(context.Background(), input, false, Mapping{
		Name: "fixture", Table: "Sysmon", Source: "sysmon", TimestampPath: "ts",
		FieldPaths: map[string]string{"EventType": "kind", "Host": "host.name", "Message": "message"},
	})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Dataset.EventCount != 2 {
		t.Fatalf("event count = %d, want 2", result.Dataset.EventCount)
	}
	var timestamp, host string
	if err := store.DB().QueryRow("SELECT time_generated, host FROM events ORDER BY id LIMIT 1").Scan(&timestamp, &host); err != nil {
		t.Fatal(err)
	}
	if timestamp != "2024-01-02T01:04:05.000000000Z" || host != "pc-1" {
		t.Fatalf("stored timestamp, host = %q, %q", timestamp, host)
	}
}

func TestImportGzipJSONArray(t *testing.T) {
	store := openTestStore(t)
	service := New(store)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	writer.Write([]byte(`[{"ts":1704067200,"message":"one"},{"ts":1704067260000,"message":"two"}]`))
	writer.Close()

	result, err := service.Import(context.Background(), &compressed, true, Mapping{
		Name: "compressed", Table: "Compressed", Source: "test", TimestampPath: "ts", FieldPaths: map[string]string{"Message": "message"},
	})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Dataset.EventCount != 2 {
		t.Fatalf("event count = %d, want 2", result.Dataset.EventCount)
	}
}

func TestImportNormalizesEmbeddedJSONAndCatalogsFields(t *testing.T) {
	store := openTestStore(t)
	service := New(store)
	input := strings.NewReader(`{"ts":"2024-01-01T00:00:00Z","AuditData":"{\"ClientIP\":\"192.0.2.1\",\"Success\":true}"}`)
	if _, err := service.Import(context.Background(), input, false, Mapping{Name: "nested", Table: "Nested", Source: "test", TimestampPath: "ts"}); err != nil {
		t.Fatal(err)
	}
	var clientIP string
	if err := store.DB().QueryRow(`SELECT json_extract(raw_data, '$.AuditData.ClientIP') FROM events`).Scan(&clientIP); err != nil {
		t.Fatal(err)
	}
	if clientIP != "192.0.2.1" {
		t.Fatalf("nested ClientIP = %q", clientIP)
	}
	fields, err := store.ListFields(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, field := range fields {
		if field.Path == "RawData.AuditData.ClientIP" && field.Type == "string" {
			found = true
		}
	}
	if !found {
		t.Fatalf("field catalog = %#v, missing nested ClientIP", fields)
	}
}

func TestImportFastPathPreservesRawJSONAndCatalogsFields(t *testing.T) {
	store := openTestStore(t)
	raw := `{"ts":"2024-01-01T00:00:00Z","count":1,"score":1.5,"nested":{"enabled":true},"items":[1,2]}`
	if _, err := New(store).Import(t.Context(), strings.NewReader(raw), false, Mapping{
		Name: "raw", Table: "Raw", Source: "test", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := store.DB().QueryRow("SELECT raw_data FROM events").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != raw {
		t.Fatalf("stored raw data changed:\n got %s\nwant %s", stored, raw)
	}
	fields, err := store.ListFields(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"RawData.count":          "long",
		"RawData.score":          "real",
		"RawData.nested":         "dynamic",
		"RawData.nested.enabled": "bool",
		"RawData.items":          "dynamic",
	}
	for _, field := range fields {
		if fieldType, exists := want[field.Path]; exists && field.Type == fieldType {
			delete(want, field.Path)
		}
	}
	if len(want) != 0 {
		t.Fatalf("field catalog = %#v, missing %#v", fields, want)
	}
}

func TestImportRollsBackOnInvalidRecord(t *testing.T) {
	store := openTestStore(t)
	service := New(store)
	_, err := service.Import(context.Background(), strings.NewReader(`
{"ts":"2024-01-01T00:00:00Z"}
{"message":"missing timestamp"}
	`), false, Mapping{Name: "bad", Table: "Bad", Source: "test", TimestampPath: "ts"})
	if err == nil || !strings.Contains(err.Error(), "record 2") {
		t.Fatalf("error = %v, want record 2 error", err)
	}
	var datasets, events int
	store.DB().QueryRow("SELECT COUNT(*) FROM datasets").Scan(&datasets)
	store.DB().QueryRow("SELECT COUNT(*) FROM events").Scan(&events)
	if datasets != 0 || events != 0 {
		t.Fatalf("partial import remained: %d datasets, %d events", datasets, events)
	}
}

func TestImportRollsBackFlushedBatchOnInvalidRecord(t *testing.T) {
	store := openTestStore(t)
	var input strings.Builder
	for index := 0; index < eventInsertBatchSize+1; index++ {
		fmt.Fprintf(&input, "{\"ts\":\"2024-01-01T00:00:00Z\",\"index\":%d}\n", index)
	}
	input.WriteString("{\"message\":\"missing timestamp\"}\n")
	_, err := New(store).Import(t.Context(), strings.NewReader(input.String()), false, Mapping{
		Name: "batched", Table: "Batched", Source: "test", TimestampPath: "ts",
	})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("record %d", eventInsertBatchSize+2)) {
		t.Fatalf("error = %v, want final record error", err)
	}
	var datasets, events int
	store.DB().QueryRow("SELECT COUNT(*) FROM datasets").Scan(&datasets)
	store.DB().QueryRow("SELECT COUNT(*) FROM events").Scan(&events)
	if datasets != 0 || events != 0 {
		t.Fatalf("flushed batch remained: %d datasets, %d events", datasets, events)
	}
}

func TestImportCSVMapsFieldsAndNormalizesCells(t *testing.T) {
	store := openTestStore(t)
	input := strings.NewReader("\uFEFFts,source,kind,host,message,AuditData,serial\n" +
		`2024-01-02T03:04:05Z,endpoint,login,pc-1,"hello, world","{""ClientIP"":""192.0.2.10""}",00123` + "\n" +
		`2024-01-02T03:05:05Z,endpoint,process,pc-2,"line one` + "\n" + `line two","{""ClientIP"":""192.0.2.11""}",00456` + "\n")
	result, err := New(store).Import(t.Context(), input, false, Mapping{
		Name: "csv", Table: "CSV", Format: FormatCSV, SourcePath: "source", TimestampPath: "ts", TimestampFormat: "rfc3339",
		FieldPaths: map[string]string{"EventType": "kind", "Host": "host", "Message": "message"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Dataset.EventCount != 2 {
		t.Fatalf("event count = %d, want 2", result.Dataset.EventCount)
	}
	var source, host, message, clientIP, serial string
	if err := store.DB().QueryRow(`SELECT source, host, message, json_extract(raw_data, '$.AuditData.ClientIP'), json_extract(raw_data, '$.serial') FROM events ORDER BY id LIMIT 1`).Scan(&source, &host, &message, &clientIP, &serial); err != nil {
		t.Fatal(err)
	}
	if source != "endpoint" || host != "pc-1" || message != "hello, world" || clientIP != "192.0.2.10" || serial != "00123" {
		t.Fatalf("stored CSV values = %q, %q, %q, %q, %q", source, host, message, clientIP, serial)
	}
}

func TestImportCSVRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		format  string
		message string
	}{
		{name: "duplicate header", input: "ts,ts\n2024-01-01T00:00:00Z,x\n", format: FormatCSV, message: "appears more than once"},
		{name: "empty header", input: "ts,  \n2024-01-01T00:00:00Z,x\n", format: FormatCSV, message: "header column 2 is empty"},
		{name: "wrong field count", input: "ts,message\n2024-01-01T00:00:00Z\n", format: FormatCSV, message: "wrong number of fields"},
		{name: "unsupported format", input: "", format: "tsv", message: "unsupported input format"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := openTestStore(t)
			_, err := New(store).Import(t.Context(), strings.NewReader(test.input), false, Mapping{
				Name: "bad", Table: "Bad", Format: test.format, Source: "test", TimestampPath: "ts",
			})
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want %q", err, test.message)
			}
			var datasets int
			if scanErr := store.DB().QueryRow("SELECT COUNT(*) FROM datasets").Scan(&datasets); scanErr != nil {
				t.Fatal(scanErr)
			}
			if datasets != 0 {
				t.Fatalf("failed import left %d dataset(s)", datasets)
			}
		})
	}
}

func openTestStore(t *testing.T) *database.Store {
	t.Helper()
	store, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
