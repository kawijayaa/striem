package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Velocidex/ordereddict"
	"github.com/kawijayaa/striem/internal/database"
)

func TestImportPreservesTimestampAndRawFields(t *testing.T) {
	store := openTestStore(t)
	service := New(store)
	input := strings.NewReader(`
{"ts":"2024-01-02T03:04:05+02:00","kind":"process","host":{"name":"pc-1"},"message":"created"}
{"ts":"2024-01-02T03:05:05+02:00","kind":"network","host":{"name":"pc-1"},"message":"connected"}
`)
	result, err := service.Import(context.Background(), input, false, Mapping{
		Name: "fixture", Table: "Sysmon", Source: "sysmon", TimestampPath: "ts",
	})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Dataset.EventCount != 2 {
		t.Fatalf("event count = %d, want 2", result.Dataset.EventCount)
	}
	var timestamp, host string
	if err := store.DB().QueryRow("SELECT time_generated, json_extract(raw_data, '$.host.name') FROM events ORDER BY id LIMIT 1").Scan(&timestamp, &host); err != nil {
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
		Name: "compressed", Table: "Compressed", Source: "test", TimestampPath: "ts",
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
		if field.Path == "AuditData.ClientIP" && field.Type == "string" {
			found = true
		}
	}
	if !found {
		t.Fatalf("field catalog = %#v, missing nested ClientIP", fields)
	}
}

func TestImportMinifiesRawJSONAndCatalogsFields(t *testing.T) {
	store := openTestStore(t)
	raw := `{ "ts": "2024-01-01T00:00:00Z", "count": 1, "score": 1.5, "nested": { "enabled": true }, "items": [1, 2] }`
	wantRaw := `{"ts":"2024-01-01T00:00:00Z","count":1,"score":1.5,"nested":{"enabled":true},"items":[1,2]}`
	if _, err := New(store).Import(t.Context(), strings.NewReader(raw), false, Mapping{
		Name: "raw", Table: "Raw", Source: "test", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := store.DB().QueryRow("SELECT raw_data FROM events").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != wantRaw {
		t.Fatalf("stored raw data = %s, want minified %s", stored, wantRaw)
	}
	fields, err := store.ListFields(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"count":          "long",
		"score":          "real",
		"nested":         "dynamic",
		"nested.enabled": "bool",
		"items":          "dynamic",
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
	input := strings.NewReader("\uFEFF\"ts\",\"source\",\"kind\",\"host\",\"message\",\"AuditData\",\"serial\"\n" +
		`2024-01-02T03:04:05Z,endpoint,login,pc-1,"hello, world","{""ClientIP"":""192.0.2.10""}",00123` + "\n" +
		`2024-01-02T03:05:05Z,endpoint,process,pc-2,"line one` + "\n" + `line two","{""ClientIP"":""192.0.2.11""}",00456` + "\n")
	result, err := New(store).Import(t.Context(), input, false, Mapping{
		Name: "csv", Table: "CSV", Format: FormatCSV, SourcePath: "source", TimestampPath: "ts", TimestampFormat: "rfc3339",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Dataset.EventCount != 2 {
		t.Fatalf("event count = %d, want 2", result.Dataset.EventCount)
	}
	var source, host, message, clientIP, serial string
	if err := store.DB().QueryRow(`SELECT source, json_extract(raw_data, '$.host'), json_extract(raw_data, '$.message'), json_extract(raw_data, '$.AuditData.ClientIP'), json_extract(raw_data, '$.serial') FROM events ORDER BY id LIMIT 1`).Scan(&source, &host, &message, &clientIP, &serial); err != nil {
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

func TestImportEVTXPreservesFields(t *testing.T) {
	store := openTestStore(t)
	input, err := os.Open(filepath.Join("testdata", "security-one-record.evtx"))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()

	result, err := New(store).Import(t.Context(), input, false, Mapping{
		Name: "security", Table: "Security", Format: FormatEVTX,
		SourcePath: "System.Provider.Name", TimestampPath: "System.TimeCreated.SystemTime",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Dataset.EventCount == 0 {
		t.Fatal("EVTX import contained no events")
	}

	var timestamp, source, eventType, host, user, provider string
	if err := store.DB().QueryRow(`
SELECT time_generated, source,
       json_extract(raw_data, '$.System.EventID.Value'),
       json_extract(raw_data, '$.System.Computer'),
       json_extract(raw_data, '$.UserData.LogFileCleared.SubjectUserName'),
       json_extract(raw_data, '$.System.Provider.Name')
FROM events ORDER BY id LIMIT 1`).Scan(&timestamp, &source, &eventType, &host, &user, &provider); err != nil {
		t.Fatal(err)
	}
	if timestamp == "" || source != "Microsoft-Windows-Eventlog" || eventType != "1102" || host != "TestComputer" || user != "test" || provider != source {
		t.Fatalf("stored EVTX values = %q, %q, %q, %q, %q, %q", timestamp, source, eventType, host, user, provider)
	}
}

func TestImportGzipEVTX(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join("testdata", "security-one-record.evtx"))
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	temporaryDirectory := t.TempDir()
	t.Setenv("STRIEM_DATA_DIR", temporaryDirectory)

	result, err := New(openTestStore(t)).Import(t.Context(), &compressed, true, Mapping{
		Name: "compressed-evtx", Table: "CompressedEVTX", Format: FormatEVTX,
		Source: "windows", TimestampPath: "System.TimeCreated.SystemTime",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Dataset.EventCount == 0 {
		t.Fatal("compressed EVTX import contained no events")
	}
	entries, err := os.ReadDir(temporaryDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("EVTX import left temporary files: %v", entries)
	}
}

func TestImportEVTXRejectsInvalidInput(t *testing.T) {
	store := openTestStore(t)
	_, err := New(store).Import(t.Context(), strings.NewReader("not an EVTX file"), false, Mapping{
		Name: "invalid-evtx", Table: "InvalidEVTX", Format: FormatEVTX,
		Source: "windows", TimestampPath: "System.TimeCreated.SystemTime",
	})
	if err == nil || !strings.Contains(err.Error(), "EVTX header") {
		t.Fatalf("Import() error = %v, want EVTX header error", err)
	}
	var datasets int
	if err := store.DB().QueryRow("SELECT COUNT(*) FROM datasets").Scan(&datasets); err != nil {
		t.Fatal(err)
	}
	if datasets != 0 {
		t.Fatalf("failed EVTX import left %d dataset(s)", datasets)
	}
}

func TestFieldDiscoverySampleCutoffAndUnseenKeySets(t *testing.T) {
	store := openTestStore(t)
	var input strings.Builder
	for index := 0; index < fieldDiscoverySampleRecords; index++ {
		fmt.Fprintf(&input, `{"ts":"2024-01-01T00:00:00Z","nested":{"sample":%d}}`+"\n", index)
	}
	input.WriteString(`{"ts":"2024-01-01T00:00:00Z","nested":{"lateKnown":true}}` + "\n")
	input.WriteString(`{"ts":"2024-01-01T00:00:00Z","nested":{"lateUnseen":true},"variant":"new-key-set"}` + "\n")
	if _, err := New(store).Import(t.Context(), strings.NewReader(input.String()), false, Mapping{
		Name: "sampling", Table: "Sampling", Source: "test", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}

	fields, err := store.ListFields(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := make(map[string]bool, len(fields))
	for _, field := range fields {
		found[field.Path] = true
	}
	if found["nested.lateKnown"] {
		t.Fatal("record 5001 with a known top-level key set was fully discovered")
	}
	if !found["nested.lateUnseen"] || !found["variant"] {
		t.Fatalf("unseen key-set fields were not discovered: %#v", fields)
	}
}

func TestFieldPathCacheReusesParentKeyEntry(t *testing.T) {
	discovery := newFieldDiscovery()
	first := discovery.cachedFieldPath("RawData.parent", "field.with.dot")
	second := discovery.cachedFieldPath("RawData.parent", "field.with.dot")
	if first != second || len(discovery.pathCache["RawData.parent"]) != 1 {
		t.Fatalf("cached paths = %q, %q, %#v", first, second, discovery.pathCache)
	}
}

func TestRootFieldPathsPromoteOnlyIdentifiers(t *testing.T) {
	if got := appendFieldPath("", "event_type"); got != "event_type" {
		t.Fatalf("identifier path = %q", got)
	}
	if got := appendFieldPath("", "名前"); got != "名前" {
		t.Fatalf("Unicode identifier path = %q", got)
	}
	if got := appendFieldPath("", "field.with.dots"); got != `RawData["field.with.dots"]` {
		t.Fatalf("escaped root path = %q", got)
	}
}

func TestCatalogueFieldsEscapesReservedAndAmbiguousRoots(t *testing.T) {
	discovery := newFieldDiscovery()
	discovery.fields = map[string]string{
		"Source":       "dynamic",
		"Source.name":  "string",
		"Alpha":        "string",
		"alpha":        "dynamic",
		"alpha.value":  "long",
		"valid_name":   "bool",
		`RawData["x"]`: "string",
	}

	fields := discovery.catalogueFields()
	for path, fieldType := range map[string]string{
		`RawData["Source"]`:      "dynamic",
		`RawData["Source"].name`: "string",
		`RawData["Alpha"]`:       "string",
		`RawData["alpha"]`:       "dynamic",
		`RawData["alpha"].value`: "long",
		"valid_name":             "bool",
		`RawData["x"]`:           "string",
	} {
		if fields[path] != fieldType {
			t.Errorf("field %q = %q, want %q; all fields: %#v", path, fields[path], fieldType, fields)
		}
	}
	if len(fields) != 7 {
		t.Fatalf("catalogue fields = %#v", fields)
	}
}

func TestImportNormalizesKnownEmbeddedPathAfterSample(t *testing.T) {
	store := openTestStore(t)
	var input strings.Builder
	input.WriteString(`{ "ts": "2024-01-01T00:00:00Z", "payload": "{\"nested\":\"{\\\"seed\\\":true}\"}" }` + "\n")
	for index := 1; index < fieldDiscoverySampleRecords; index++ {
		input.WriteString(`{"ts":"2024-01-01T00:00:00Z","payload":"plain"}` + "\n")
	}
	input.WriteString(`{ "ts": "2024-01-01T00:00:00Z", "payload": "{ \"precise\": 123456789012345678901234567890, \"nested\": \"{\\\"ok\\\":true}\" }" }` + "\n")
	if _, err := New(store).Import(t.Context(), strings.NewReader(input.String()), false, Mapping{
		Name: "known-embedded", Table: "KnownEmbedded", Source: "test", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := store.DB().QueryRow("SELECT raw_data FROM events ORDER BY id DESC LIMIT 1").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	want := `{"payload":{"nested":{"ok":true},"precise":123456789012345678901234567890},"ts":"2024-01-01T00:00:00Z"}`
	if raw != want {
		t.Fatalf("normalized raw data = %s, want %s", raw, want)
	}
}

func TestConfiguredMaxInputBytes(t *testing.T) {
	t.Setenv("STRIEM_MAX_INPUT_BYTES", "")
	if limit, err := configuredMaxInputBytes(); err != nil || limit != defaultMaxExpandedSize {
		t.Fatalf("default limit = %d, %v", limit, err)
	}
	t.Setenv("STRIEM_MAX_INPUT_BYTES", " 12345 ")
	if limit, err := configuredMaxInputBytes(); err != nil || limit != 12345 {
		t.Fatalf("configured limit = %d, %v", limit, err)
	}
	for _, value := range []string{"0", "-1", "1MiB", "9223372036854775808"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("STRIEM_MAX_INPUT_BYTES", value)
			if _, err := configuredMaxInputBytes(); err == nil || !strings.Contains(err.Error(), "positive integer number of bytes") {
				t.Fatalf("configuredMaxInputBytes() error = %v", err)
			}
		})
	}
}

func TestImportEnforcesInputLimitAndRestoresSynchronous(t *testing.T) {
	store := openTestStore(t)
	store.DB().SetMaxOpenConns(1)
	t.Setenv("STRIEM_MAX_INPUT_BYTES", "32")
	_, err := New(store).Import(t.Context(), strings.NewReader(`{"ts":"2024-01-01T00:00:00Z","padding":"too long"}`), false, Mapping{
		Name: "limited", Table: "Limited", Source: "test", TimestampPath: "ts",
	})
	if err == nil || !strings.Contains(err.Error(), "32 byte limit") {
		t.Fatalf("Import() error = %v, want input limit error", err)
	}
	var synchronous int
	if err := store.DB().QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if synchronous != 1 {
		t.Fatalf("PRAGMA synchronous = %d, want NORMAL (1)", synchronous)
	}
	t.Setenv("STRIEM_MAX_INPUT_BYTES", "1024")
	if _, err := New(store).Import(t.Context(), strings.NewReader(`{"ts":"2024-01-01T00:00:00Z"}`), false, Mapping{
		Name: "successful", Table: "Successful", Source: "test", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if synchronous != 1 {
		t.Fatalf("PRAGMA synchronous after success = %d, want NORMAL (1)", synchronous)
	}
}

func TestImportRestoresPreviousSynchronousSetting(t *testing.T) {
	store := openTestStore(t)
	store.DB().SetMaxOpenConns(1)
	store.DB().SetMaxIdleConns(1)
	if _, err := store.DB().Exec("PRAGMA synchronous = FULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := New(store).Import(t.Context(), strings.NewReader(`{"ts":"2024-01-01T00:00:00Z"}`), false, Mapping{
		Name: "synchronous", Table: "Synchronous", Source: "test", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}
	var synchronous int
	if err := store.DB().QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if synchronous != 2 {
		t.Fatalf("PRAGMA synchronous = %d, want FULL (2)", synchronous)
	}
}

func TestDecodeCSVPreservesHeaderOrderAndJSONEscaping(t *testing.T) {
	input := "z,\"a\"\"b\",line\n\"<tag>&\",\"quote\"\"\\path\",\"first\nsecond\"\n"
	var raw string
	count, err := decodeCSVRecords(t.Context(), strings.NewReader(input), func(_ int, record json.RawMessage) error {
		raw = string(record)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"z":"\u003ctag\u003e\u0026","a\"b":"quote\"\\path","line":"first\nsecond"}`
	if count != 1 || raw != want {
		t.Fatalf("decoded CSV = %d, %s, want 1, %s", count, raw, want)
	}
}

func TestMarshalEVTXEventUsesInnerOrderedDict(t *testing.T) {
	inner := ordereddict.NewDict().Set("System", ordereddict.NewDict().Set("Computer", "host")).Set("EventData", "value")
	wrapper := ordereddict.NewDict().Set("Event", inner)
	raw, err := marshalEVTXEvent(wrapper, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"System":{"Computer":"host"},"EventData":"value"}`
	if string(raw) != want {
		t.Fatalf("marshaled EVTX event = %s, want %s", raw, want)
	}
}

func TestImportParallelPreparationPreservesRecordOrder(t *testing.T) {
	store := openTestStore(t)
	const count = fieldDiscoverySampleRecords + 250
	var input strings.Builder
	for index := 1; index <= count; index++ {
		fmt.Fprintf(&input, `{"ts":"2024-01-01T00:00:00Z","sequence":%d}`+"\n", index)
	}
	if _, err := New(store).Import(t.Context(), strings.NewReader(input.String()), false, Mapping{
		Name: "ordered", Table: "Ordered", Source: "test", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := store.DB().Query("SELECT json_extract(raw_data, '$.sequence') FROM events ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		index++
		var sequence int
		if err := rows.Scan(&sequence); err != nil {
			t.Fatal(err)
		}
		if sequence != index {
			t.Fatalf("record %d stored sequence %d", index, sequence)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if index != count {
		t.Fatalf("stored %d records, want %d", index, count)
	}
}

func TestImportReportsLowestIndexedParallelError(t *testing.T) {
	store := openTestStore(t)
	var input strings.Builder
	input.WriteString(`{"ts":"2024-01-01T00:00:00Z","payload":"{\"seed\":true}"}` + "\n")
	for index := 1; index < fieldDiscoverySampleRecords; index++ {
		input.WriteString(`{"ts":"2024-01-01T00:00:00Z","payload":"plain"}` + "\n")
	}
	slowPayload := `{"padding":` + strconv.Quote(strings.Repeat("x", 512<<10)) + `}`
	fmt.Fprintf(&input, `{"ts":"invalid","payload":%s}`+"\n", strconv.Quote(slowPayload))
	input.WriteString(`{"ts":"invalid","payload":"{}"}` + "\n")
	_, err := New(store).Import(t.Context(), strings.NewReader(input.String()), false, Mapping{
		Name: "lowest-error", Table: "LowestError", Source: "test", TimestampPath: "ts",
	})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("record %d timestamp", fieldDiscoverySampleRecords+1)) {
		t.Fatalf("Import() error = %v, want lowest indexed record", err)
	}
}

func BenchmarkImportNDJSON(b *testing.B) {
	const eventCount = 20_000
	var input strings.Builder
	for index := 0; index < eventCount; index++ {
		fmt.Fprintf(&input, `{"ts":"2024-01-01T00:00:00Z","event":{"type":"process","host":"pc-%d","user":"user-%d"},"message":"created process","values":[1,2,3]}`+"\n", index%100, index%10)
	}
	payload := input.String()
	store := openTestStore(b)
	if err := store.DropEventIndexes(b.Context()); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		result, err := New(store).Import(b.Context(), strings.NewReader(payload), false, Mapping{
			Name: fmt.Sprintf("benchmark-%d", iteration), Table: fmt.Sprintf("Benchmark%d", iteration),
			Source: "benchmark", TimestampPath: "ts", TimestampFormat: "rfc3339",
		})
		if err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if _, err := store.DB().ExecContext(b.Context(), "DELETE FROM datasets WHERE id = ?", result.Dataset.ID); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
	b.ReportMetric(float64(eventCount)/b.Elapsed().Seconds()*float64(b.N), "events/s")
}

func BenchmarkImportTestdata(b *testing.B) {
	tests := []struct {
		name    string
		file    string
		mapping Mapping
	}{
		{
			name: "Microsoft365CSV", file: "events.csv",
			mapping: Mapping{
				Format: FormatCSV, Source: "microsoft365", TimestampPath: "CreationDate", TimestampFormat: "2/01/2006 3:04:05 PM",
			},
		},
		{
			name: "SysmonNDJSON", file: "sysmon.ndjson",
			mapping: Mapping{
				Source: "sysmon", TimestampPath: "Event.System.TimeCreated.#attributes.SystemTime", TimestampFormat: "rfc3339",
			},
		},
		{
			name: "SuricataJSON", file: "eve.json",
			mapping: Mapping{
				Source: "suricata", TimestampPath: "timestamp", TimestampFormat: "2006-01-02T15:04:05.999999-0700",
			},
		},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			path := filepath.Join("..", "..", "testdata", test.file)
			info, err := os.Stat(path)
			if err != nil {
				b.Skipf("testdata unavailable: %v", err)
			}
			store := openTestStore(b)
			if err := store.DropEventIndexes(b.Context()); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(info.Size())
			var imported int64
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				input, err := os.Open(path)
				if err != nil {
					b.Fatal(err)
				}
				mapping := test.mapping
				mapping.Name = fmt.Sprintf("%s-%d", test.name, iteration)
				mapping.Table = fmt.Sprintf("%s%d", test.name, iteration)
				result, importErr := New(store).Import(b.Context(), input, false, mapping)
				closeErr := input.Close()
				if importErr != nil {
					b.Fatal(importErr)
				}
				if closeErr != nil {
					b.Fatal(closeErr)
				}
				imported += result.Dataset.EventCount
				b.StopTimer()
				if _, err := store.DB().ExecContext(b.Context(), "DELETE FROM datasets WHERE id = ?", result.Dataset.ID); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
			b.ReportMetric(float64(imported)/b.Elapsed().Seconds(), "events/s")
			b.ReportMetric(float64(imported)/float64(b.N), "events/op")
		})
	}
}

func openTestStore(t testing.TB) *database.Store {
	t.Helper()
	store, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
