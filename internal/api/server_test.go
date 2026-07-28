package api

import (
	"bytes"
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
	store, server := testServer(t)
	events := `{"ts":"2024-01-01T00:00:00Z","host":"pc-1","process":{"name":"powershell.exe"}}
{"ts":"2024-01-01T00:01:00Z","host":"pc-2","process":{"name":"cmd.exe"}}`
	if _, err := ingest.New(store).Import(t.Context(), strings.NewReader(events), false, ingest.Mapping{
		Name: "demo", Table: "Sysmon", Source: "sysmon", TimestampPath: "ts", TimestampFormat: "auto",
		FieldPaths: map[string]string{"EventType": "kind", "Host": "host", "User": "user", "Message": "message"},
	}); err != nil {
		t.Fatal(err)
	}

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
	store, server := testServer(t)
	events := `{"ts":"2024-01-01T00:00:00Z","host":"pc-1","user":"alice","ip":"198.51.100.7"}
{"ts":"2024-01-01T00:01:00Z","host":"pc-2","user":"bob","ip":"198.51.100.7"}
{"ts":"2024-01-01T00:02:00Z","host":"pc-3","user":"alice","ip":"192.0.2.4"}`
	if _, err := ingest.New(store).Import(t.Context(), strings.NewReader(events), false, ingest.Mapping{
		Name: "audit", Table: "UAL", Source: "audit", TimestampPath: "ts",
		FieldPaths: map[string]string{"Host": "host", "User": "user"},
	}); err != nil {
		t.Fatal(err)
	}

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

func testServer(t *testing.T) (*database.Store, *httptest.Server) {
	t.Helper()
	store, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	t.Cleanup(func() {
		server.Close()
		store.Close()
	})
	return store, server
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
