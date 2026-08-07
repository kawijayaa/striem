package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kawijayaa/striem/internal/database"
	"github.com/kawijayaa/striem/internal/ingest"
)

func TestProvisionedDataCanBeQueried(t *testing.T) {
	store := testStore(t)
	events := `{"ts":"2024-01-01T00:00:00Z","host":"pc-1","process":{"name":"powershell.exe"}}
{"ts":"2024-01-01T00:01:00Z","host":"pc-2","process":{"name":"cmd.exe"}}`
	if _, err := ingest.New(store).Import(t.Context(), strings.NewReader(events), false, ingest.Mapping{
		Name: "demo", Table: "Sysmon", Source: "sysmon", TimestampPath: "ts", TimestampFormat: "auto",
		FieldPaths: map[string]string{"EventType": "kind", "Host": "host", "User": "user", "Message": "message"},
	}); err != nil {
		t.Fatal(err)
	}
	server := serveStore(t, store)

	response := queryAPI(t, server.URL, `Sysmon | extend Process=tostring(RawData.process.name) | where Process contains "powershell" | project Host, Process`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query status = %d: %s", response.StatusCode, body)
	}
	var result struct {
		Columns []string         `json:"columns"`
		Rows    []map[string]any `json:"rows"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Columns, ",") != "Host,Process" || len(result.Rows) != 1 || result.Rows[0]["Host"] != "pc-1" || result.Rows[0]["Process"] != "powershell.exe" {
		t.Fatalf("result = %#v", result)
	}
}

func TestKSQLAggregatesAndDynamicResults(t *testing.T) {
	store := testStore(t)
	events := `{"ts":"2024-01-01T00:00:00Z","host":"pc-1","user":"alice","ip":"198.51.100.7"}
{"ts":"2024-01-01T00:01:00Z","host":"pc-2","user":"bob","ip":"198.51.100.7"}
{"ts":"2024-01-01T00:02:00Z","host":"pc-3","user":"alice","ip":"192.0.2.4"}`
	if _, err := ingest.New(store).Import(t.Context(), strings.NewReader(events), false, ingest.Mapping{
		Name: "audit", Table: "UAL", Source: "audit", TimestampPath: "ts",
		FieldPaths: map[string]string{"Host": "host", "User": "user"},
	}); err != nil {
		t.Fatal(err)
	}
	server := serveStore(t, store)

	rows := queryRows(t, server.URL, `UAL | extend ClientIP=tostring(RawData.ip) | summarize Failures=count() by ClientIP | order by Failures desc`)
	if len(rows) != 2 || rows[0]["ClientIP"] != "198.51.100.7" || rows[0]["Failures"] != float64(2) {
		t.Fatalf("aggregate rows = %#v", rows)
	}
	dynamicRows := queryRows(t, server.URL, `UAL | take 1 | project Payload=RawData`)
	payload, ok := dynamicRows[0]["Payload"].(map[string]any)
	if !ok || payload["host"] != "pc-1" {
		t.Fatalf("dynamic payload = %#v", dynamicRows)
	}
}

func TestQueryCatalogIsLoadedOnce(t *testing.T) {
	store := testStore(t)
	if _, err := ingest.New(store).Import(t.Context(), strings.NewReader(`{"ts":"2024-01-01T00:00:00Z"}`), false, ingest.Mapping{
		Name: "first", Table: "First", Source: "first", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}
	server := serveStore(t, store)

	if _, err := ingest.New(store).Import(t.Context(), strings.NewReader(`{"ts":"2024-01-01T00:00:00Z"}`), false, ingest.Mapping{
		Name: "second", Table: "Second", Source: "second", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}

	response := queryAPI(t, server.URL, `First | count`)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("cached table query status = %d", response.StatusCode)
	}
	response = queryAPI(t, server.URL, `Second | count`)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("post-startup table query status = %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
}

func TestScanRowsPreservesDynamicJSON(t *testing.T) {
	store := testStore(t)
	object := `  {"large":9007199254740993, "decimal":2.50}  `
	array := ` [1, {"ok":true}] `
	scalar := ` 42 `
	malformed := ` {"missing": `
	rows, err := store.DB().QueryContext(t.Context(), `SELECT ? AS Object, ? AS Array, ? AS Scalar, ? AS Malformed`, object, array, scalar, malformed)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	_, results, err := scanRows(rows, map[string]struct{}{"Object": {}, "Array": {}, "Scalar": {}, "Malformed": {}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("rows = %d, want 1", len(results))
	}
	if raw, ok := results[0]["Object"].(json.RawMessage); !ok || string(raw) != strings.TrimSpace(object) {
		t.Fatalf("object = %#v", results[0]["Object"])
	}
	if raw, ok := results[0]["Array"].(json.RawMessage); !ok || string(raw) != strings.TrimSpace(array) {
		t.Fatalf("array = %#v", results[0]["Array"])
	}
	if results[0]["Scalar"] != scalar || results[0]["Malformed"] != malformed {
		t.Fatalf("scalar or malformed value changed: %#v", results[0])
	}

	recorder := httptest.NewRecorder()
	writeJSON(recorder, http.StatusOK, results[0])
	body := recorder.Body.String()
	if !strings.Contains(body, `"large":9007199254740993`) || !strings.Contains(body, `"decimal":2.50`) {
		t.Fatalf("dynamic JSON representation changed: %s", body)
	}
}

func TestQueryPrepareBehavior(t *testing.T) {
	type errorResponse struct {
		Error    string `json:"error"`
		Position struct {
			Line   int `json:"line"`
			Column int `json:"column"`
		} `json:"position"`
	}

	store := testStore(t)
	apiServer := New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	prepareCalls := 0
	prepareQuery := apiServer.prepareQuery
	apiServer.prepareQuery = func(ctx context.Context, query string) (*sql.Stmt, error) {
		prepareCalls++
		return prepareQuery(ctx, query)
	}
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	response := queryAPI(t, server.URL, `Events | take 1`)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || prepareCalls != 0 {
		t.Fatalf("query status = %d, prepare calls = %d", response.StatusCode, prepareCalls)
	}

	response = queryAPIPath(t, server.URL+"/api/query/validate", `Events | take 1`)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent || prepareCalls != 1 {
		t.Fatalf("validation status = %d, prepare calls = %d", response.StatusCode, prepareCalls)
	}

	response = queryAPI(t, server.URL, `Events | summarize Nested=sum(count())`)
	var queryError errorResponse
	if err := json.NewDecoder(response.Body).Decode(&queryError); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || queryError.Error != "query contains an invalid column or expression" || queryError.Position.Line != 1 || queryError.Position.Column != 1 || prepareCalls != 1 {
		t.Fatalf("invalid query status = %d, response = %#v, prepare calls = %d", response.StatusCode, queryError, prepareCalls)
	}

	response = queryAPIPath(t, server.URL+"/api/query/validate", `Events | summarize Nested=sum(count())`)
	queryError = errorResponse{}
	if err := json.NewDecoder(response.Body).Decode(&queryError); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || queryError.Error != "query contains an invalid column or expression" || queryError.Position.Line != 1 || queryError.Position.Column != 1 || prepareCalls != 2 {
		t.Fatalf("invalid validation status = %d, response = %#v, prepare calls = %d", response.StatusCode, queryError, prepareCalls)
	}
}

func TestQueryValidation(t *testing.T) {
	_, server := testServer(t)

	response := queryAPIPath(t, server.URL+"/api/query/validate", `Events | take 1`)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("valid query status = %d", response.StatusCode)
	}

	for _, query := range []string{
		`Events | where`,
		`Events | parse Message with "user=" UserName:string`,
		`Missing | take 1`,
	} {
		response := queryAPIPath(t, server.URL+"/api/query/validate", query)
		if response.StatusCode != http.StatusBadRequest {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			t.Fatalf("query %q status = %d: %s", query, response.StatusCode, body)
		}
		var result struct {
			Error    string `json:"error"`
			Position struct {
				Line   int `json:"line"`
				Column int `json:"column"`
			} `json:"position"`
		}
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if result.Error == "" || result.Position.Line < 1 || result.Position.Column < 1 {
			t.Fatalf("diagnostic = %#v", result)
		}
	}
}

func TestQueryRequestValidationRejectsMalformedBodies(t *testing.T) {
	_, server := testServer(t)
	tests := []struct {
		name        string
		contentType string
		body        string
		status      int
	}{
		{name: "wrong content type", contentType: "text/plain", body: `{"query":"Events"}`, status: http.StatusUnsupportedMediaType},
		{name: "malformed JSON", contentType: "application/json", body: `{`, status: http.StatusBadRequest},
		{name: "missing query", contentType: "application/json", body: `{}`, status: http.StatusBadRequest},
		{name: "unknown property", contentType: "application/json", body: `{"query":"Events","extra":true}`, status: http.StatusBadRequest},
		{name: "trailing object", contentType: "application/json", body: `{"query":"Events"} {}`, status: http.StatusBadRequest},
		{name: "oversized query", contentType: "application/json", body: string(mustJSON(t, map[string]string{"query": strings.Repeat("x", 32<<10+1)})), status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, server.URL+"/api/query/validate", strings.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", test.contentType)
			request.Header.Set("X-Striem-Request", "1")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatalf("status = %d, want %d", response.StatusCode, test.status)
			}
		})
	}
}

func TestStateChangingRequestsRequireSameOrigin(t *testing.T) {
	_, server := testServer(t)
	tests := []struct {
		name   string
		header map[string]string
		status int
	}{
		{name: "missing verification header", header: map[string]string{"Content-Type": "application/json"}, status: http.StatusForbidden},
		{name: "cross-site fetch", header: map[string]string{"Content-Type": "application/json", "X-Striem-Request": "1", "Sec-Fetch-Site": "cross-site"}, status: http.StatusForbidden},
		{name: "foreign origin", header: map[string]string{"Content-Type": "application/json", "X-Striem-Request": "1", "Origin": "https://attacker.example"}, status: http.StatusForbidden},
		{name: "same origin", header: map[string]string{"Content-Type": "application/json", "X-Striem-Request": "1", "Origin": server.URL}, status: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, server.URL+"/api/query", strings.NewReader(`{"query":"Events | take 1"}`))
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range test.header {
				request.Header.Set(name, value)
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatalf("status = %d, want %d", response.StatusCode, test.status)
			}
		})
	}
}

func TestSchemaAdvertisesEnabledKSQLFeatures(t *testing.T) {
	_, server := testServer(t)
	response, err := http.Get(server.URL + "/api/schema")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var schema struct {
		Operators []string `json:"operators"`
		Functions []string `json:"functions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&schema); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"where", "search", "project", "project-away", "project-rename", "summarize", "sample", "mv-expand", "mv-apply", "join"} {
		if !contains(schema.Operators, value) {
			t.Fatalf("operators do not include %q: %v", value, schema.Operators)
		}
	}
	for _, value := range []string{"tostring", "countif", "parse_json", "array_length"} {
		if !contains(schema.Functions, value) {
			t.Fatalf("functions do not include %q: %v", value, schema.Functions)
		}
	}
	if contains(schema.Functions, "dcount") {
		t.Fatalf("schema advertises unsupported features: %#v", schema)
	}
}

func TestHealthFieldsAndSchemaReflectProvisionedStore(t *testing.T) {
	store := testStore(t)
	if _, err := store.DB().ExecContext(t.Context(), `
INSERT INTO datasets(id, name, table_name, input_signature, source, timestamp_path, event_count, created_at)
VALUES (1, 'Zulu audit', 'Zulu', 'z', 'audit', 'ts', 4, '2026-08-07T00:00:00Z'),
       (2, 'Alpha logs', 'Alpha', 'a', 'sysmon', 'ts', 3, '2026-08-07T00:00:00Z'),
       (3, 'Legacy data', '', 'legacy', 'legacy', 'ts', 2, '2026-08-07T00:00:00Z');
INSERT INTO dataset_fields(dataset_id, path, type)
VALUES (1, 'RawData.User', 'string'),
       (2, 'RawData.EventID', 'number'),
       (3, 'RawData.Hidden', 'string');`); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChallengeName(t.Context(), "Coverage Challenge"); err != nil {
		t.Fatal(err)
	}
	server := serveStore(t, store)

	for _, path := range []string{"/api/health", "/api/ready"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		var health map[string]string
		if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
			response.Body.Close()
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK || health["status"] != "ok" {
			t.Fatalf("%s status = %d, body = %#v", path, response.StatusCode, health)
		}
	}

	response, err := http.Get(server.URL + "/api/fields")
	if err != nil {
		t.Fatal(err)
	}
	var fields struct {
		Common []database.Field      `json:"common"`
		Tables []database.FieldGroup `json:"tables"`
	}
	if err := json.NewDecoder(response.Body).Decode(&fields); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(fields.Common) != 7 || len(fields.Tables) != 2 {
		t.Fatalf("fields status = %d, body = %#v", response.StatusCode, fields)
	}
	if fields.Tables[0].Table != "Alpha" || fields.Tables[0].Fields[0].Path != "RawData.EventID" || fields.Tables[1].Table != "Zulu" {
		t.Fatalf("field groups = %#v", fields.Tables)
	}

	response, err = http.Get(server.URL + "/api/schema")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		ChallengeName string `json:"challengeName"`
		Tables        []struct {
			Name       string `json:"name"`
			EventCount int64  `json:"eventCount"`
		} `json:"tables"`
	}
	if err := json.NewDecoder(response.Body).Decode(&schema); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || schema.ChallengeName != "Coverage Challenge" || len(schema.Tables) != 3 {
		t.Fatalf("schema status = %d, body = %#v", response.StatusCode, schema)
	}
	if schema.Tables[0].Name != "Events" || schema.Tables[0].EventCount != 9 || schema.Tables[1].Name != "Alpha" || schema.Tables[2].Name != "Zulu" {
		t.Fatalf("schema tables = %#v", schema.Tables)
	}
}

func TestReadEndpointsReportUnavailableDatabase(t *testing.T) {
	store := testStore(t)
	apiServer := New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	for _, path := range []string{"/api/health", "/api/ready", "/api/fields", "/api/schema"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusServiceUnavailable && response.StatusCode != http.StatusInternalServerError {
			t.Errorf("%s status = %d, want an unavailable-server response", path, response.StatusCode)
		}
	}

	response, err := http.Get(server.URL + "/api/questions")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("/api/questions status = %d, want in-memory state to remain available", response.StatusCode)
	}
}

func testServer(t *testing.T) (*database.Store, *httptest.Server) {
	t.Helper()
	store := testStore(t)
	return store, serveStore(t, store)
}

func testStore(t *testing.T) *database.Store {
	t.Helper()
	store, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func serveStore(t *testing.T, store *database.Store) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	t.Cleanup(server.Close)
	return server
}

func queryRows(t *testing.T, serverURL, query string) []map[string]any {
	t.Helper()
	response := queryAPI(t, serverURL, query)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query status = %d: %s", response.StatusCode, body)
	}
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result.Rows
}

func queryAPI(t *testing.T, serverURL, query string) *http.Response {
	t.Helper()
	return queryAPIPath(t, serverURL+"/api/query", query)
}

func queryAPIPath(t *testing.T, url, query string) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Striem-Request", "1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
