package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kawijayaa/striem/internal/database"
	"github.com/kawijayaa/striem/internal/ingest"
	"github.com/kawijayaa/striem/internal/kql"
)

func TestProvisionedDataCanBeQueried(t *testing.T) {
	store := testStore(t)
	events := `{"ts":"2024-01-01T00:00:00Z","host":"pc-1","process":{"name":"powershell.exe"}}
{"ts":"2024-01-01T00:01:00Z","host":"pc-2","process":{"name":"cmd.exe"}}`
	if _, err := ingest.New(store).Import(t.Context(), strings.NewReader(events), false, ingest.Mapping{
		Name: "demo", Table: "Sysmon", Source: "sysmon", TimestampPath: "ts", TimestampFormat: "auto",
	}); err != nil {
		t.Fatal(err)
	}
	server := serveStore(t, store)

	response := queryAPI(t, server.URL, `Sysmon | extend Computer=tostring(host), ProcessName=tostring(process.name) | where ProcessName contains "powershell" | project Computer, ProcessName`)
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
	if strings.Join(result.Columns, ",") != "Computer,ProcessName" || len(result.Rows) != 1 || result.Rows[0]["Computer"] != "pc-1" || result.Rows[0]["ProcessName"] != "powershell.exe" {
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
	}); err != nil {
		t.Fatal(err)
	}
	server := serveStore(t, store)

	rows := queryRows(t, server.URL, `UAL | summarize Failures=count() by ClientIP=ip | order by Failures desc`)
	if len(rows) != 2 || rows[0]["ClientIP"] != "198.51.100.7" || rows[0]["Failures"] != float64(2) {
		t.Fatalf("aggregate rows = %#v", rows)
	}
	dynamicRows := queryRows(t, server.URL, `UAL | take 1 | project Payload=RawData`)
	payload, ok := dynamicRows[0]["Payload"].(map[string]any)
	if !ok || payload["host"] != "pc-1" {
		t.Fatalf("dynamic payload = %#v", dynamicRows)
	}
}

func TestLogicalRootColumnsPreserveJSONTypes(t *testing.T) {
	store := testStore(t)
	if _, err := ingest.New(store).Import(t.Context(), strings.NewReader(`{"ts":"2024-01-01T00:00:00Z","count":7,"score":2.5,"enabled":true,"nested":{"value":"ok"}}`), false, ingest.Mapping{
		Name: "typed", Table: "Typed", Source: "fixture", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}
	server := serveStore(t, store)
	rows := queryRows(t, server.URL, `Typed | project count, score, enabled, nested`)
	if len(rows) != 1 || rows[0]["count"] != float64(7) || rows[0]["score"] != 2.5 || rows[0]["enabled"] != true {
		t.Fatalf("typed row = %#v", rows)
	}
	nested, ok := rows[0]["nested"].(map[string]any)
	if !ok || nested["value"] != "ok" {
		t.Fatalf("nested value = %#v", rows[0]["nested"])
	}
}

func TestMixedLogicalRootColumnPreservesPerRowTypes(t *testing.T) {
	store := testStore(t)
	events := `{"ts":"2024-01-01T00:00:00Z","variant":true}
{"ts":"2024-01-01T00:01:00Z","variant":"text"}`
	if _, err := ingest.New(store).Import(t.Context(), strings.NewReader(events), false, ingest.Mapping{
		Name: "mixed", Table: "Mixed", Source: "fixture", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}
	server := serveStore(t, store)
	rows := queryRows(t, server.URL, `Mixed | order by TimeGenerated asc | project variant`)
	if len(rows) != 2 || rows[0]["variant"] != true || rows[1]["variant"] != "text" {
		t.Fatalf("mixed rows = %#v", rows)
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

	_, results, err := scanRows(rows, map[string]struct{}{"Object": {}, "Array": {}, "Scalar": {}, "Malformed": {}}, nil)
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
	if raw, ok := results[0]["Scalar"].(json.RawMessage); !ok || string(raw) != strings.TrimSpace(scalar) {
		t.Fatalf("scalar = %#v", results[0]["Scalar"])
	}
	if results[0]["Malformed"] != malformed {
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
	if response.StatusCode != http.StatusBadRequest || queryError.Error != "count requires a summarize aggregation context" || queryError.Position.Line != 1 || queryError.Position.Column != 31 || prepareCalls != 1 {
		t.Fatalf("invalid query status = %d, response = %#v, prepare calls = %d", response.StatusCode, queryError, prepareCalls)
	}

	response = queryAPIPath(t, server.URL+"/api/query/validate", `Events | summarize Nested=sum(count())`)
	queryError = errorResponse{}
	if err := json.NewDecoder(response.Body).Decode(&queryError); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || queryError.Error != "count requires a summarize aggregation context" || queryError.Position.Line != 1 || queryError.Position.Column != 31 || prepareCalls != 1 {
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
	for _, value := range []string{"where", "search", "project", "project-away", "project-rename", "summarize", "sample", "mv-expand", "mv-apply", "join", "invoke", "parse", "parse-where", "parse-kv", "render"} {
		if !contains(schema.Operators, value) {
			t.Fatalf("operators do not include %q: %v", value, schema.Operators)
		}
	}
	for _, value := range []string{"toscalar", "materialize", "arg_max", "arg_min", "argmax", "argmin", "tostring", "countif", "parse_json", "parse_csv", "parse_xml", "extract_all", "extractall", "parsejson", "todynamic", "toobject", "array_length", "parse_url", "parseurl", "parse_urlquery", "parseurlquery"} {
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
       (3, 'Legacy data', '', 'legacy', 'legacy', 'ts', 2, '2026-08-07T00:00:00Z'),
       (4, 'Alpha logs two', 'Alpha', 'a2', 'sysmon-two', 'ts', 2, '2026-08-07T00:00:00Z');
INSERT INTO dataset_fields(dataset_id, path, type)
VALUES (1, 'User', 'string'),
       (2, 'EventID', 'long'),
       (4, 'Computer', 'string'),
       (3, 'Hidden', 'string');`); err != nil {
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
	if response.StatusCode != http.StatusOK || len(fields.Common) != 3 || len(fields.Tables) != 2 {
		t.Fatalf("fields status = %d, body = %#v", response.StatusCode, fields)
	}
	if fields.Tables[0].Table != "Alpha" || len(fields.Tables[0].Fields) != 2 || fields.Tables[0].Fields[0].Path != "Computer" || fields.Tables[0].Fields[1].Path != "EventID" || fields.Tables[1].Table != "Zulu" {
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
	if schema.Tables[0].Name != "Events" || schema.Tables[0].EventCount != 11 || schema.Tables[1].Name != "Alpha" || schema.Tables[1].EventCount != 5 || schema.Tables[2].Name != "Zulu" {
		t.Fatalf("schema tables = %#v", schema.Tables)
	}
}

func TestLoadingServerStaysOnlineUntilCatalogIsRefreshed(t *testing.T) {
	store := testStore(t)
	apiServer := New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	apiServer.SetChallengeName("Operation Loading Screen")
	apiServer.SetLoading()
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("static interface status = %d, want 200", response.StatusCode)
	}

	response, err = http.Get(server.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200 while loading", response.StatusCode)
	}

	response, err = http.Get(server.URL + "/api/ready")
	if err != nil {
		t.Fatal(err)
	}
	var loadingState map[string]string
	if err := json.NewDecoder(response.Body).Decode(&loadingState); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || loadingState["challengeName"] != "Operation Loading Screen" {
		t.Fatalf("loading readiness status = %d, body = %#v", response.StatusCode, loadingState)
	}

	for _, path := range []string{"/api/schema", "/api/fields"} {
		response, err = http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want 503 while loading", path, response.StatusCode)
		}
	}

	if _, err := ingest.New(store).Import(t.Context(), strings.NewReader(`{"ts":"2024-01-01T00:00:00Z","host":"ready"}`), false, ingest.Mapping{
		Name: "startup", Table: "Startup", Source: "fixture", TimestampPath: "ts",
	}); err != nil {
		t.Fatal(err)
	}
	if err := apiServer.RefreshCatalog(t.Context()); err != nil {
		t.Fatal(err)
	}

	response, err = http.Get(server.URL + "/api/ready")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("ready status = %d, want 200 after refresh", response.StatusCode)
	}

	rows := queryRows(t, server.URL, `Startup | project host`)
	if len(rows) != 1 || rows[0]["host"] != "ready" {
		t.Fatalf("rows after refresh = %#v", rows)
	}
}

func TestReadinessReportsStartupFailure(t *testing.T) {
	store := testStore(t)
	apiServer := New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	apiServer.SetStartupError("ingestion failed")
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	response, err := http.Get(server.URL + "/api/ready")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var state map[string]string
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusServiceUnavailable || state["status"] != "error" || state["error"] != "ingestion failed" {
		t.Fatalf("readiness failure status = %d, body = %#v", response.StatusCode, state)
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

func TestLogicalFieldsExcludeReservedInvalidAndAmbiguousNames(t *testing.T) {
	fields := logicalFields([]database.Field{
		{Path: "Alpha", Type: "string"},
		{Path: "alpha", Type: "long"},
		{Path: "TimeGenerated", Type: "string"},
		{Path: "Source", Type: "string"},
		{Path: "RawData", Type: "dynamic"},
		{Path: "nested.value", Type: "string"},
		{Path: `RawData["invalid-name"]`, Type: "string"},
		{Path: "valid_name", Type: "bool"},
		{Path: "名前", Type: "string"},
	})
	if len(fields) != 2 || fields[0] != (kql.Field{Name: "valid_name", Type: "bool"}) || fields[1] != (kql.Field{Name: "名前", Type: "string"}) {
		t.Fatalf("logical fields = %#v", fields)
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

func TestScanRowsNormalizesNonFiniteNumbers(t *testing.T) {
	store := testStore(t)
	rows, err := store.DB().QueryContext(t.Context(), `SELECT 1e999 AS PositiveInfinity, -1e999 AS NegativeInfinity, 2.5 AS Finite`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	_, results, err := scanRows(rows, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0]["PositiveInfinity"] != nil || results[0]["NegativeInfinity"] != nil || results[0]["Finite"] != 2.5 {
		t.Fatalf("unexpected numeric results: %#v", results)
	}
	recorder := httptest.NewRecorder()
	writeJSON(recorder, http.StatusOK, results)
	if !json.Valid(recorder.Body.Bytes()) {
		t.Fatalf("invalid JSON response: %q", recorder.Body.String())
	}
}

func TestJSONEndpointsRequireExactMediaType(t *testing.T) {
	_, server := testServer(t)
	for _, path := range []string{"/api/query", "/api/query/validate", "/api/questions/missing/answer"} {
		for _, contentType := range []string{"application/jsonp", "application/json-invalid", "application/json; charset"} {
			t.Run(path+"/"+contentType, func(t *testing.T) {
				request, err := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(`{"query":"Events | take 1"}`))
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("Content-Type", contentType)
				request.Header.Set("X-Striem-Request", "1")
				response, err := http.DefaultClient.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != http.StatusUnsupportedMediaType {
					t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusUnsupportedMediaType)
				}
			})
		}
	}
}

func TestNotAndHuntingFunctionsThroughAPI(t *testing.T) {
	store := testStore(t)
	if _, err := ingest.New(store).Import(t.Context(), strings.NewReader(`{"ts":"2024-01-01T00:15:00Z","host":"one"}
{"ts":"2024-01-01T00:45:00Z","host":"two"}`), false, ingest.Mapping{Name: "not fixture", Table: "Audit", Source: "fixture", TimestampPath: "ts"}); err != nil {
		t.Fatal(err)
	}
	server := serveStore(t, store)
	for _, query := range []string{
		`Audit | project Negated=not(host == "one") | order by Negated asc`,
		`Audit | extend Negated=not(host == "one") | where not(false) | project Renamed=Negated | order by Renamed asc`,
	} {
		rows := queryRows(t, server.URL, query)
		key := "Negated"
		if strings.Contains(query, "Renamed") {
			key = "Renamed"
		}
		if len(rows) != 2 || rows[0][key] != false || rows[1][key] != true {
			t.Fatalf("Boolean metadata lost: %#v", rows)
		}
	}
	rows := queryRows(t, server.URL, `Audit | extend X=not(true) | extend X=2 | project X | take 1`)
	if rows[0]["X"] != float64(2) {
		t.Fatalf("stale Boolean metadata: %#v", rows)
	}
	rows = queryRows(t, server.URL, `Audit | where not(host == "missing") | summarize N=dcount(host) by bin(TimeGenerated,1h)`)
	if len(rows) != 1 || rows[0]["N"] != float64(2) {
		t.Fatalf("bucketed distinct counts: %#v", rows)
	}
}

func TestBagAggregatesThroughAPI(t *testing.T) {
	store := testStore(t)
	if _, err := ingest.New(store).Import(t.Context(), strings.NewReader(`{"ts":"2024-01-01T00:00:00Z","bag":{"a":1,"nested":[true,null]},"keep":true}
{"ts":"2024-01-01T00:01:00Z","bag":{"b":2},"keep":false}`), false, ingest.Mapping{Name: "bags", Table: "Bags", Source: "fixture", TimestampPath: "ts"}); err != nil {
		t.Fatal(err)
	}
	server := serveStore(t, store)
	rows := queryRows(t, server.URL, `Bags | summarize Result=make_bag_if(bag,keep) | project Renamed=Result`)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	bag, ok := rows[0]["Renamed"].(map[string]any)
	if !ok || len(bag) != 2 || bag["a"] != float64(1) {
		t.Fatalf("expected object, got %#v", rows)
	}
	nested, ok := bag["nested"].([]any)
	if !ok || len(nested) != 2 || nested[0] != true || nested[1] != nil {
		t.Fatalf("nested values lost: %#v", bag)
	}
	rows = queryRows(t, server.URL, `Bags | where keep == false and keep == true | summarize Result=make_bag(bag)`)
	if bag, ok := rows[0]["Result"].(map[string]any); !ok || len(bag) != 0 {
		t.Fatalf("expected empty object, got %#v", rows)
	}
}

func TestSplitArraysThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print a=split("a,b",",") | project result=a`)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	values, ok := rows[0]["result"].([]any)
	if !ok || len(values) != 2 || values[0] != "a" || values[1] != "b" {
		t.Fatalf("expected JSON array, got %#v", rows)
	}
	rows = queryRows(t, server.URL, `print result=split("a,b",",",1)`)
	values, ok = rows[0]["result"].([]any)
	if !ok || len(values) != 1 || values[0] != "b" {
		t.Fatalf("expected indexed JSON array, got %#v", rows)
	}
}

func TestConstructedDynamicValuesThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print a=pack_array(true,null,bag_pack("x",1)),b=bag_pack("nested",pack_array("a","b"))`)
	array, ok := rows[0]["a"].([]any)
	if !ok || len(array) != 3 || array[0] != true || array[1] != nil {
		t.Fatalf("array=%#v", rows)
	}
	nested, ok := array[2].(map[string]any)
	if !ok || nested["x"] != float64(1) {
		t.Fatalf("nested bag=%#v", array)
	}
	bag, ok := rows[0]["b"].(map[string]any)
	if !ok {
		t.Fatal(rows)
	}
	values, ok := bag["nested"].([]any)
	if !ok || len(values) != 2 || values[0] != "a" || values[1] != "b" {
		t.Fatalf("nested array=%#v", bag)
	}
}

func TestConstructedLongPrecisionThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	response := queryAPI(t, server.URL, `print a=pack_array(9223372036854775807),b=bag_pack("long",9223372036854775807)`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query failed: %s", body)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	array := result.Rows[0]["a"].([]any)
	bag := result.Rows[0]["b"].(map[string]any)
	for _, value := range []any{array[0], bag["long"]} {
		number, ok := value.(json.Number)
		if !ok || number.String() != "9223372036854775807" {
			t.Fatalf("long precision lost: %#v", result)
		}
	}
}

func TestBagOperationsThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print b=bag_remove_keys(bag_merge(dynamic({"keep":true,"drop":1}),dynamic({"keep":false,"extra":[1,2]})),dynamic(["drop"])) | project renamed=b`)
	bag, ok := rows[0]["renamed"].(map[string]any)
	if !ok || bag["keep"] != true || len(bag) != 2 {
		t.Fatalf("bag=%#v", rows)
	}
	if values, ok := bag["extra"].([]any); !ok || len(values) != 2 {
		t.Fatalf("array=%#v", bag)
	}
}

func TestBagSetKeyThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	response := queryAPI(t, server.URL, `print b=bag_set_key(bag_set_key(dynamic({}),"$.nested.bool",true),"$.nested.long",9223372036854775807) | project renamed=b`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query failed: %s", body)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	bag, ok := result.Rows[0]["renamed"].(map[string]any)
	if !ok {
		t.Fatalf("bag=%#v", result)
	}
	nested, ok := bag["nested"].(map[string]any)
	if !ok || nested["bool"] != true || nested["long"] != json.Number("9223372036854775807") {
		t.Fatalf("nested=%#v", bag)
	}
}

func TestArrayOperationsThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	response := queryAPI(t, server.URL, `print a=array_reverse(array_concat(dynamic([9223372036854775807,null]),pack_array(true,bag_pack("x",2)))) | project renamed=a`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query failed: %s", body)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	values, ok := result.Rows[0]["renamed"].([]any)
	if !ok || len(values) != 4 || values[1] != true || values[2] != nil || values[3] != json.Number("9223372036854775807") {
		t.Fatalf("array=%#v", result)
	}
	bag, ok := values[0].(map[string]any)
	if !ok || bag["x"] != json.Number("2") {
		t.Fatalf("nested bag=%#v", values)
	}
}

func TestArraySliceThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	response := queryAPI(t, server.URL, `print a=array_slice(pack_array(0,true,9223372036854775807,null),1,-1) | project renamed=a`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query failed: %s", body)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	values, ok := result.Rows[0]["renamed"].([]any)
	if !ok || len(values) != 3 || values[0] != true || values[1] != json.Number("9223372036854775807") || values[2] != nil {
		t.Fatalf("slice=%#v", result)
	}
}

func TestArrayMovementThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	response := queryAPI(t, server.URL, `print a=array_rotate_right(pack_array(true,9223372036854775807,null),1),b=array_shift_left(pack_array(true,9223372036854775807),1,bag_pack("bool",false))`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query failed: %s", body)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	a, ok := result.Rows[0]["a"].([]any)
	if !ok || len(a) != 3 || a[0] != nil || a[1] != true || a[2] != json.Number("9223372036854775807") {
		t.Fatalf("rotation=%#v", result)
	}
	b, ok := result.Rows[0]["b"].([]any)
	if !ok || len(b) != 2 || b[0] != json.Number("9223372036854775807") {
		t.Fatalf("shift=%#v", result)
	}
	bag, ok := b[1].(map[string]any)
	if !ok || bag["bool"] != false {
		t.Fatalf("padding=%#v", b)
	}
}

func TestZipThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	response := queryAPI(t, server.URL, `print z=zip(pack_array(true,9223372036854775807),pack_array(bag_pack("x",false))) | project renamed=z`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query failed: %s", body)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	rows, ok := result.Rows[0]["renamed"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("zip=%#v", result)
	}
	first, ok := rows[0].([]any)
	if !ok || len(first) != 2 || first[0] != true {
		t.Fatalf("first=%#v", rows)
	}
	bag, ok := first[1].(map[string]any)
	if !ok || bag["x"] != false {
		t.Fatalf("bag=%#v", first)
	}
	second, ok := rows[1].([]any)
	if !ok || len(second) != 2 || second[0] != json.Number("9223372036854775807") || second[1] != nil {
		t.Fatalf("second=%#v", rows)
	}
}

func TestArraySplitThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	response := queryAPI(t, server.URL, `print a=array_split(pack_array(true,9223372036854775807,bag_pack("x",false)),1) | project renamed=a`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query failed: %s", body)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	segments, ok := result.Rows[0]["renamed"].([]any)
	if !ok || len(segments) != 2 {
		t.Fatalf("split=%#v", result)
	}
	first, ok := segments[0].([]any)
	if !ok || len(first) != 1 || first[0] != true {
		t.Fatalf("first=%#v", segments)
	}
	second, ok := segments[1].([]any)
	if !ok || len(second) != 2 || second[0] != json.Number("9223372036854775807") {
		t.Fatalf("second=%#v", segments)
	}
	bag, ok := second[1].(map[string]any)
	if !ok || bag["x"] != false {
		t.Fatalf("bag=%#v", second)
	}
}

func TestArraySumThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print a=array_sum(dynamic([0.5,0.25])),b=series_sum(dynamic([1,2,3])),c=array_sum(dynamic([1,"2"]))`)
	if len(rows) != 1 || rows[0]["a"] != float64(0.75) || rows[0]["b"] != float64(6) || rows[0]["c"] != nil {
		t.Fatalf("numeric/null values=%#v", rows)
	}
}

func TestTypedArraySearchThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print index=array_index_of(dynamic([true,1]),1),member=set_has_element(dynamic([true]),true),numeric=set_has_element(dynamic([true]),1)`)
	if len(rows) != 1 || rows[0]["index"] != float64(1) || rows[0]["member"] != true || rows[0]["numeric"] != false {
		t.Fatalf("typed lookup results=%#v", rows)
	}
}

func TestArrayIffThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	response := queryAPI(t, server.URL, `print a=array_iff(dynamic([true,false]),9223372036854775807,true),b=array_iif(dynamic([true,false]),dynamic([true]),"no")`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query failed: %s", body)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	a, ok := result.Rows[0]["a"].([]any)
	if !ok || len(a) != 2 || a[0] != json.Number("9223372036854775807") || a[1] != true {
		t.Fatalf("typed branches=%#v", result)
	}
	b, ok := result.Rows[0]["b"].([]any)
	if !ok || len(b) != 2 || b[0] != true || b[1] != "no" {
		t.Fatalf("alias=%#v", result)
	}
}

func TestRepeatThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	response := queryAPI(t, server.URL, `print a=repeat(true,2),b=repeat(9223372036854775807,2),c=repeat(1,-1),d=repeat(1,0)`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query failed: %s", body)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	a, ok := result.Rows[0]["a"].([]any)
	if !ok || len(a) != 2 || a[0] != true || a[1] != true {
		t.Fatalf("bool array=%#v", result)
	}
	b, ok := result.Rows[0]["b"].([]any)
	if !ok || len(b) != 2 || b[0] != json.Number("9223372036854775807") || b[1] != json.Number("9223372036854775807") {
		t.Fatalf("long array=%#v", result)
	}
	d, ok := result.Rows[0]["d"].([]any)
	if !ok || len(d) != 0 || result.Rows[0]["c"] != nil {
		t.Fatalf("zero/negative count=%#v", result)
	}
}

func TestBagZipThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	response := queryAPI(t, server.URL, `print b=bag_zip(dynamic(["bool","long","missing"]),pack_array(true,9223372036854775807)) | project renamed=b`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query failed: %s", body)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	bag, ok := result.Rows[0]["renamed"].(map[string]any)
	if !ok || len(bag) != 3 || bag["bool"] != true || bag["long"] != json.Number("9223372036854775807") || bag["missing"] != nil {
		t.Fatalf("bag=%#v", result)
	}
}

func TestSetOperationsThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	response := queryAPI(t, server.URL, `print a=set_union(pack_array(true,9223372036854775807),pack_array(true,bag_pack("x",false))),b=set_intersect(dynamic([1,2]),dynamic([2,3])),c=set_difference(dynamic([1,2]),dynamic([2]))`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query failed: %s", body)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	a, ok := result.Rows[0]["a"].([]any)
	if !ok || len(a) != 3 || a[0] != true || a[1] != json.Number("9223372036854775807") {
		t.Fatalf("union=%#v", result)
	}
	bag, ok := a[2].(map[string]any)
	if !ok || bag["x"] != false {
		t.Fatalf("nested value=%#v", a)
	}
	for key, want := range map[string]json.Number{"b": "2", "c": "1"} {
		values, ok := result.Rows[0][key].([]any)
		if !ok || len(values) != 1 || values[0] != want {
			t.Fatalf("%s=%#v", key, result)
		}
	}
}

func TestJaccardThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print j=jaccard_index(dynamic([1,2,3]),dynamic([1,2,3,4])) | project renamed=j`)
	if len(rows) != 1 || rows[0]["renamed"] != float64(0.75) {
		t.Fatalf("real output=%#v", rows)
	}
}

func TestURLParsingThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print u=parse_url("https://host/path?key=value"),q=parse_urlquery("a=b") | project renamed=u,q`)
	if len(rows) != 1 {
		t.Fatalf("rows=%#v", rows)
	}
	u, ok := rows[0]["renamed"].(map[string]any)
	if !ok || u["Host"] != "host" || u["Path"] != "path" {
		t.Fatalf("dynamic URL output=%#v", rows)
	}
	parameters, ok := u["Query Parameters"].(map[string]any)
	if !ok || parameters["key"] != "value" {
		t.Fatalf("nested parameters=%#v", u)
	}
	q, ok := rows[0]["q"].(map[string]any)
	if !ok {
		t.Fatalf("dynamic query output=%#v", rows)
	}
	parameters, ok = q["Query Parameters"].(map[string]any)
	if !ok || parameters["a"] != "b" {
		t.Fatalf("nested query parameters=%#v", q)
	}
}

func TestParseCSVThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print raw='a,"b,c",1,true' | project renamed=parse_csv(raw)`)
	if len(rows) != 1 {
		t.Fatalf("rows=%#v", rows)
	}
	array, ok := rows[0]["renamed"].([]any)
	if !ok || len(array) != 4 || array[0] != "a" || array[1] != "b,c" || array[2] != "1" || array[3] != "true" {
		t.Fatalf("dynamic string array=%#v", rows)
	}
	for _, query := range []string{`print parse_csv()`, `print parse_csv(1)`, `print parse_csv("a","b")`} {
		response := queryAPI(t, server.URL, query)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d", query, response.StatusCode)
		}
	}
}

func TestParseXMLThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print d=parse_xml('<e name="value"><a>one</a><a>two</a></e>'),invalid=parse_xml("bad") | project renamed=d,invalid`)
	if len(rows) != 1 || rows[0]["invalid"] != nil {
		t.Fatalf("rows=%#v", rows)
	}
	object, ok := rows[0]["renamed"].(map[string]any)
	if !ok {
		t.Fatalf("dynamic XML output=%#v", rows)
	}
	element, ok := object["e"].(map[string]any)
	if !ok || element["@name"] != "value" {
		t.Fatalf("element=%#v", object)
	}
	array, ok := element["a"].([]any)
	if !ok || len(array) != 2 || array[0] != "one" || array[1] != "two" {
		t.Fatalf("repeated elements=%#v", element)
	}
}

func TestExtractAllThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print flat=extract_all("(a)","a a"),nested=extractall("(a)(b)",dynamic([2,1]),"ab"),missing=extract_all("(a)","none") | project flat,nested,missing`)
	if len(rows) != 1 || rows[0]["missing"] != nil {
		t.Fatalf("rows=%#v", rows)
	}
	flat, ok := rows[0]["flat"].([]any)
	if !ok || len(flat) != 2 || flat[0] != "a" || flat[1] != "a" {
		t.Fatalf("flat=%#v", rows)
	}
	nested, ok := rows[0]["nested"].([]any)
	if !ok || len(nested) != 1 {
		t.Fatalf("nested=%#v", rows)
	}
	groups, ok := nested[0].([]any)
	if !ok || len(groups) != 2 || groups[0] != "b" || groups[1] != "a" {
		t.Fatalf("groups=%#v", nested)
	}
	for _, query := range []string{`print extract_all("a","a")`, `print extract_all("(a)",dynamic([0]),"a")`, `print extract_all("(a)","[1]","a")`} {
		response := queryAPI(t, server.URL, query)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d", query, response.StatusCode)
		}
	}
}

func TestScalarQueryFunctionsThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `let f=(x:bool){bag_pack("value",not(x))}; let g=(x:bool){not(x)}; print d=f(false),b=g(false) | project renamed=d,b`)
	if len(rows) != 1 || rows[0]["b"] != true {
		t.Fatalf("Boolean function output=%#v", rows)
	}
	object, ok := rows[0]["renamed"].(map[string]any)
	if !ok || object["value"] != true {
		t.Fatalf("dynamic function output=%#v", rows)
	}
	rows = queryRows(t, server.URL, `let getQueryParamValue=(querystring:string,param:string){let params=parse_urlquery(querystring); tostring(params["Query Parameters"].[param])}; print result=getQueryParamValue("view=vs-2019&preserve-view=true","preserve-view")`)
	if len(rows) != 1 || rows[0]["result"] != "true" {
		t.Fatalf("parameter accessor=%#v", rows)
	}
	for _, query := range []string{
		`let f=(x:long){x}; print result=f("bad")`,
		`let f=(){unknown}; print 1`,
		`let f=(x:long){x}; print f(x=)`,
		`let f=(x:long){x+x}; print ` + strings.Repeat("f(", 40) + "1" + strings.Repeat(")", 40),
	} {
		response := queryAPI(t, server.URL, query)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d", query, response.StatusCode)
		}
	}
}

func TestTabularQueryFunctionsThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `let f=(T:(d:dynamic,b:bool)){T|where b}; print d=dynamic({"x":181}),b=not(false) | invoke f() | project renamed=d,b`)
	if len(rows) != 1 || rows[0]["b"] != true {
		t.Fatalf("Boolean table result=%#v", rows)
	}
	object, ok := rows[0]["renamed"].(map[string]any)
	if !ok || object["x"] != float64(181) {
		t.Fatalf("dynamic table result=%#v", rows)
	}
	rows = queryRows(t, server.URL, `let f=(T:(x:long),minimum:long=2){T|where x>=minimum}; f(datatable(x:long)[1,2,3]) | count`)
	if len(rows) != 1 || rows[0]["Count"] != float64(2) {
		t.Fatalf("table filter=%#v", rows)
	}
	for _, query := range []string{
		`let f=(T:(x:long)){T}; f((print y=1))`,
		`let f=(T:(*)){T|project hidden}; print 1`,
		`let f=(T:(*),x:long){T}; print 1 | invoke f()`,
		`print 1 | invoke`,
		`let f=(T:(x:long)){union T,T}; f(` + strings.Repeat("f(", 25) + `(print x=1)` + strings.Repeat(")", 26),
	} {
		response := queryAPI(t, server.URL, query)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d", query, response.StatusCode)
		}
	}
}

func TestQuotedColumnDeclarationsThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print ['a.b']=dynamic({"x":193}),['bool value']=not(false) | project-rename ['renamed data']=['a.b'] | project ['renamed data'],['bool value']`)
	if len(rows) != 1 || rows[0]["bool value"] != true {
		t.Fatalf("quoted Boolean result=%#v", rows)
	}
	object, ok := rows[0]["renamed data"].(map[string]any)
	if !ok || object["x"] != float64(193) {
		t.Fatalf("quoted dynamic result=%#v", rows)
	}
	rows = queryRows(t, server.URL, `let f=(T:(['some column']:long)){T}; f(datatable(['some' ' column']:long)[197])`)
	if len(rows) != 1 || rows[0]["some column"] != float64(197) {
		t.Fatalf("quoted datatable result=%#v", rows)
	}
	for _, query := range []string{
		`datatable(['a']:long,a:long)[1,2]`, `print ['a']=`,
		`let a=dynamic({"b":1}); print ['a.b']`,
		`let f=(['a']:long){['a']}; print f(['a']=)`,
	} {
		response := queryAPI(t, server.URL, query)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d", query, response.StatusCode)
		}
	}
}

func TestQuotedJoinKeysAndLookupThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print ['left key']=1,d=dynamic([1]),b=not(false) | lookup (print ['right key']=1,d=dynamic([2]),b=not(true)) on $left.['left key']==$right.['right key'] | project d,d1,b,b1`)
	if len(rows) != 1 || rows[0]["b"] != true || rows[0]["b1"] != false {
		t.Fatalf("lookup Boolean columns=%#v", rows)
	}
	first, ok := rows[0]["d"].([]any)
	second, otherOK := rows[0]["d1"].([]any)
	if !ok || !otherOK || len(first) != 1 || len(second) != 1 || first[0] != float64(1) || second[0] != float64(2) {
		t.Fatalf("lookup dynamic columns=%#v", rows)
	}
	for _, query := range []string{
		`print k=1 | join kind=inner (print k=1) on $right.k==$left.k`,
		`print k=1 | join kind=inner (print k=1) on $left.k>$right.k`,
		`print k=1 | join kind=inner (print k=1) on $left.k== $right.k or $left.k==$right.k`,
		`print k=1 | join kind=inner (print k="a") on $left.k==$right.k`,
		`print k=1 | join kind=inner (print k=1) where $left.k==`,
		`print k=1 | lookup kind=rightsemi (print k=1) on k`,
		`print k=1 | lookup (print k=1) on ,`,
		`print k=1 | join kind=inner (print k=1) on ` + strings.Repeat(`$left.k==$right.k and `, 16) + `$left.k==$right.k`,
	} {
		response := queryAPI(t, server.URL, query)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d", query, response.StatusCode)
		}
	}
}

func TestSetEqualsThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print same=set_equals(dynamic([2,1,2]),dynamic([1,2])),different=set_equals(dynamic([9007199254740992]),dynamic([9007199254740993])),empty=set_equals(dynamic([]),dynamic([])) | project same,different,empty`)
	if len(rows) != 1 || rows[0]["same"] != true || rows[0]["different"] != false || rows[0]["empty"] != true {
		t.Fatalf("Boolean output=%#v", rows)
	}
	for _, query := range []string{`print set_equals(dynamic([]))`, `print set_equals("[]",dynamic([]))`, `print set_equals(dynamic([]),dynamic([]),dynamic([]))`} {
		response := queryAPI(t, server.URL, query)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("query %s: status=%d", query, response.StatusCode)
		}
	}
}

func TestParseJSONThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	response := queryAPI(t, server.URL, `print invalid=parse_json("{bad}"),number=parse_json("9223372036854775807"),b=parse_json("false"),a=todynamic("[1,null]"),s=toobject("\"hello\""),last=parse_json('[1,9007199254740993]')[(-1)]`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("query failed: %s", body)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	row := result.Rows[0]
	if row["invalid"] != "{bad}" || row["number"] != json.Number("9223372036854775807") || row["b"] != false || row["s"] != "hello" || row["last"] != json.Number("9007199254740993") {
		t.Fatalf("dynamic scalar values=%#v", row)
	}
	values, ok := row["a"].([]any)
	if !ok || len(values) != 2 || values[0] != json.Number("1") || values[1] != nil {
		t.Fatalf("array=%#v", row)
	}
}

func TestDynamicAccessorNamesThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print d=dynamic({"a.b":{"x":71}}) | project d.['a.b'].x | project ['d_a.b_x']`)
	if len(rows) != 1 || rows[0]["d_a.b_x"] != float64(71) {
		t.Fatalf("rows=%#v", rows)
	}
}

func TestParseJSONImplicitNameThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `let raw='{"a":97}'; print root=1 | project parse_json(raw) | project raw.a`)
	if len(rows) != 1 || rows[0]["raw_a"] != float64(97) {
		t.Fatalf("rows=%#v", rows)
	}
}

func TestPrintPositionalNamesThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print explicit=1,tostring("hello"),parse_json('{"a":3}').a`)
	if len(rows) != 1 || rows[0]["explicit"] != float64(1) || rows[0]["print_1"] != "hello" || rows[0]["print_2"] != float64(3) {
		t.Fatalf("rows=%#v", rows)
	}
}

func TestUnnamedExtendThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print Column1=5 | extend parse_json('{"a":101}') | project Column1,Column2`)
	if len(rows) != 1 || rows[0]["Column1"] != float64(5) {
		t.Fatalf("rows=%#v", rows)
	}
	object, ok := rows[0]["Column2"].(map[string]any)
	if !ok || object["a"] != float64(101) {
		t.Fatalf("dynamic column=%#v", rows)
	}
}

func TestProjectionWidthLimitThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	columns := make([]string, 257)
	for i := range columns {
		columns[i] = fmt.Sprintf("c%d=%d", i, i)
	}
	response := queryAPI(t, server.URL, "print "+strings.Join(columns, ","))
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "MaxProjectionItems") {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	rows := queryRows(t, server.URL, "print "+strings.Join(columns[:256], ","))
	if len(rows) != 1 || len(rows[0]) != 256 || rows[0]["c255"] != float64(255) {
		t.Fatalf("boundary rows=%#v", rows)
	}
}

func TestDynamicGroupingRejectedThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	for _, query := range []string{`print d=dynamic({"a":1}) | summarize n=count() by d`, `print d=dynamic([1]) | distinct d`, `print d=dynamic([1]) | distinct *`} {
		response := queryAPI(t, server.URL, query)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "dynamic column") {
			t.Fatalf("status=%d body=%s", response.StatusCode, body)
		}
	}
	rows := queryRows(t, server.URL, `print d=dynamic({"a":1}) | distinct tostring(d)`)
	if len(rows) != 1 || rows[0]["d"] != `{"a":1}` {
		t.Fatalf("cast rows=%#v", rows)
	}
}

func TestParenthesizedTabularLetThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `let T=((print d=dynamic({"a":109}),b=not(false))); T | project d,b`)
	if len(rows) != 1 || rows[0]["b"] != true {
		t.Fatalf("rows=%#v", rows)
	}
	object, ok := rows[0]["d"].(map[string]any)
	if !ok || object["a"] != float64(109) {
		t.Fatalf("dynamic=%#v", rows)
	}
}

func TestQuotedLetNamesThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `let ['a.b']=dynamic({"x":131}); let ['T U']=(print value=['a.b'].x,b=not(false)); ['T U'] | project value,b`)
	if len(rows) != 1 || rows[0]["value"] != float64(131) || rows[0]["b"] != true {
		t.Fatalf("rows=%#v", rows)
	}
}

func TestDirectTabularLetAliasesThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `let T=print d=dynamic({"x":151}),b=not(false); let ['T U']=T; ['T U']`)
	if len(rows) != 1 || rows[0]["b"] != true {
		t.Fatalf("rows=%#v", rows)
	}
	d, ok := rows[0]["d"].(map[string]any)
	if !ok || d["x"] != float64(151) {
		t.Fatalf("dynamic=%#v", rows)
	}
	for _, query := range []string{`let T=NoSuchTable; T`, `let T=Events; let T=Events; T`, `let x=1; let x=Events; x`} {
		response := queryAPI(t, server.URL, query)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("query %s: status=%d", query, response.StatusCode)
		}
	}
}

func TestParenthesizedPipelineSourcesThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `((print d=dynamic({"x":163}),b=not(false)) | where b) | project d,b`)
	if len(rows) != 1 || rows[0]["b"] != true {
		t.Fatalf("rows=%#v", rows)
	}
	d, ok := rows[0]["d"].(map[string]any)
	if !ok || d["x"] != float64(163) {
		t.Fatalf("dynamic=%#v", rows)
	}
	for _, query := range []string{`(Events | take 1`, `() | take 1`, `(print x=(1]) | project x`, `(1+2) | project x`} {
		response := queryAPI(t, server.URL, query)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("query %s: status=%d", query, response.StatusCode)
		}
	}
}

// Results beyond Kusto's 500,000-record truncation limit are rejected
// rather than buffered without bound.
func TestScanRowsRejectsOversizedResults(t *testing.T) {
	store := testStore(t)
	rows, err := store.DB().QueryContext(t.Context(), `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM n WHERE x < 500001) SELECT x FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if _, _, err := scanRows(rows, nil, nil); err != errResultTooLarge {
		t.Fatalf("err = %v, want errResultTooLarge", err)
	}
	rows, err = store.DB().QueryContext(t.Context(), `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM n WHERE x < 10) SELECT x FROM n`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if _, results, err := scanRows(rows, nil, nil); err != nil || len(results) != 10 {
		t.Fatalf("results %d err %v", len(results), err)
	}
}

// Every response carries the browser protections, and API responses are
// never cached.
func TestSecurityHeaders(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	for _, path := range []string{"/", "/api/health", "/api/questions"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		header := recorder.Header()
		if !strings.Contains(header.Get("Content-Security-Policy"), "frame-ancestors 'none'") || header.Get("X-Content-Type-Options") != "nosniff" ||
			header.Get("X-Frame-Options") != "DENY" || header.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s headers = %v", path, header)
		}
		if strings.HasPrefix(path, "/api/") && header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s Cache-Control = %q", path, header.Get("Cache-Control"))
		}
	}
}

// Integers beyond JavaScript's safe range reach the browser exactly.
func TestUnsafeIntegersAreExact(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print big = hash("World"), small = 42, edge = 9007199254740991, over = 9007199254740992, negative = -9223372036854775807`)
	want := map[string]any{"big": "1846988464401551951", "small": float64(42), "edge": float64(9007199254740991), "over": "9007199254740992", "negative": "-9223372036854775807"}
	for key, value := range want {
		if rows[0][key] != value {
			t.Errorf("%s = %#v, want %#v", key, rows[0][key], value)
		}
	}
}
