package api

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kawijayaa/striem/internal/database"
	"github.com/kawijayaa/striem/internal/deployment"
	"github.com/kawijayaa/striem/internal/ingest"
)

func TestProvisionedDataCanBeQueried(t *testing.T) {
	store, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	events := `{"ts":"2024-01-01T00:00:00Z","host":"pc-1","process":{"name":"powershell.exe"}}
{"ts":"2024-01-01T00:01:00Z","host":"pc-2","process":{"name":"cmd.exe"}}`
	if _, err := ingest.New(store).Import(t.Context(), strings.NewReader(events), false, ingest.Mapping{
		Name: "demo", Table: "Sysmon", Source: "sysmon", TimestampPath: "ts", TimestampFormat: "auto",
		FieldPaths: map[string]string{"EventType": "kind", "Host": "host", "User": "user", "Message": "message"},
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()

	queryBody := bytes.NewBufferString(`{"query":"Sysmon | extend Process=tostring(RawData.process.name) | where Process contains 'powershell' | project Host, Process"}`)
	response, err := http.Post(server.URL+"/api/query", "application/json", queryBody)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		text, _ := io.ReadAll(response.Body)
		t.Fatalf("query status = %d: %s", response.StatusCode, text)
	}
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["Host"] != "pc-1" || result.Rows[0]["Process"] != "powershell.exe" {
		t.Fatalf("rows = %#v", result.Rows)
	}

	fieldsResponse, err := http.Get(server.URL + "/api/fields")
	if err != nil {
		t.Fatal(err)
	}
	defer fieldsResponse.Body.Close()
	var fields struct {
		Tables []database.FieldGroup `json:"tables"`
	}
	if err := json.NewDecoder(fieldsResponse.Body).Decode(&fields); err != nil {
		t.Fatal(err)
	}
	if len(fields.Tables) != 1 || fields.Tables[0].Table != "Sysmon" {
		t.Fatalf("field tables = %#v, want Sysmon", fields.Tables)
	}

	schemaResponse, err := http.Get(server.URL + "/api/schema")
	if err != nil {
		t.Fatal(err)
	}
	defer schemaResponse.Body.Close()
	var schema struct {
		Tables []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			EventCount  int64  `json:"eventCount"`
		} `json:"tables"`
	}
	if err := json.NewDecoder(schemaResponse.Body).Decode(&schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Tables) != 2 || schema.Tables[0].Name != "Events" || schema.Tables[1].Name != "Sysmon" {
		t.Fatalf("schema tables = %#v", schema.Tables)
	}
	if schema.Tables[0].EventCount != 2 || schema.Tables[0].Description != "All datasets" || schema.Tables[1].EventCount != 2 || schema.Tables[1].Description != "demo" {
		t.Fatalf("schema table metadata = %#v", schema.Tables)
	}
}

func TestMicrosoft365FixtureCanBeInvestigated(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	fixtureDirectory := t.TempDir()
	fixturePath := filepath.Join(fixtureDirectory, "events.csv")
	fixture, err := os.Create(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := csv.NewWriter(fixture)
	if err := writer.Write([]string{"CreationDate", "Operations", "UserIds", "RecordType", "AuditData"}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 120; index++ {
		operation := "UserLoggedIn"
		clientIP := "203.0.113.10"
		if index < 7 {
			operation = "UserLoginFailed"
			clientIP = "198.51.100.77"
		} else if index < 10 {
			operation = "UserLoginFailed"
			clientIP = "192.0.2.44"
		}
		auditData := fmt.Sprintf(`{"ClientIP":%q}`, clientIP)
		if err := writer.Write([]string{"1/01/2024 1:00:00 AM", operation, "analyst@example.com", "AzureActiveDirectory", auditData}); err != nil {
			t.Fatal(err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Close(); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(fixtureDirectory, "datasets.json")
	manifest := map[string]any{"datasets": []map[string]any{{
		"name": "Northstar Microsoft 365 audit logs", "table": "UAL", "path": fixturePath,
		"format": "csv", "source": "microsoft365", "timestampPath": "CreationDate",
		"timestampFormat": "2/01/2006 3:04:05 PM",
		"fieldPaths":      map[string]string{"EventType": "Operations", "User": "UserIds", "Message": "RecordType"},
	}}}
	if err := os.WriteFile(manifestPath, mustJSON(t, manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := deployment.Load(t.Context(), store, manifestPath)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if len(loaded) != 1 || loaded[0].EventCount != 120 {
		t.Fatalf("loaded datasets = %#v, want one with 120 events", loaded)
	}

	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()
	query := `UAL
| where EventType == "UserLoginFailed"
| extend ClientIP=tostring(RawData.AuditData.ClientIP)
| summarize Failures=count() by ClientIP
| order by Failures desc`
	response, err := http.Post(server.URL+"/api/query", "application/json", bytes.NewBuffer(mustJSON(t, map[string]string{"query": query})))
	if err != nil {
		t.Fatal(err)
	}
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
	want := map[string]float64{"198.51.100.77": 7, "192.0.2.44": 3}
	for _, row := range result.Rows {
		clientIP, failures := row["ClientIP"].(string), row["Failures"].(float64)
		if want[clientIP] != failures {
			t.Fatalf("failed sign-in group = %#v, want %s=%v", row, clientIP, want[clientIP])
		}
		delete(want, clientIP)
	}
	if len(want) != 0 || len(result.Rows) != 2 {
		t.Fatalf("failed sign-in groups = %#v, want exact groups for two IPs", result.Rows)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestExtendReplacesExistingColumn(t *testing.T) {
	store, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.DB().Exec(`INSERT INTO datasets(id,name,table_name,source,timestamp_path,created_at,event_count) VALUES(1,'x','Test','x','ts','2024-01-01T00:00:00Z',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO events(dataset_id,time_generated,source,host,raw_data) VALUES(1,'2024-01-01T00:00:00.000000000Z','x','old','{}')`); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()

	response, err := http.Post(server.URL+"/api/query", "application/json", bytes.NewBufferString(`{"query":"Events | extend Host='new' | where Host == 'new' | project Host"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["Host"] != "new" {
		t.Fatalf("rows = %#v", result.Rows)
	}
}

func TestExpandedKQLExpressionsExecute(t *testing.T) {
	store, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.DB().Exec(`INSERT INTO datasets(id,name,table_name,source,timestamp_path,created_at,event_count) VALUES(1,'x','Test','x','ts','2024-01-01T00:00:00Z',2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO events(dataset_id,time_generated,source,host,username,message,raw_data) VALUES
		(1,'2024-01-01T00:00:00.000000000Z','x','low','alice','alpha','{"score":1}'),
		(1,'2024-01-01T00:01:00.000000000Z','x','high',NULL,NULL,'{"score":3}')`); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()
	query := `let multiplier = 2;
let fallback = "unknown";
let rows = 1;
Events
| extend Score=toint(RawData.score) * multiplier, Label=strcat(coalesce(User, fallback), ":", substring(Message, 0, 3)), Kind=iff(Message == null, "missing", "present")
| top rows by Score desc
| project Host, Score, Label, Kind`
	response, err := http.Post(server.URL+"/api/query", "application/json", bytes.NewBuffer(mustJSON(t, map[string]string{"query": query})))
	if err != nil {
		t.Fatal(err)
	}
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
	if len(result.Rows) != 1 || result.Rows[0]["Host"] != "high" || result.Rows[0]["Score"] != float64(6) || result.Rows[0]["Label"] != "unknown:" || result.Rows[0]["Kind"] != "missing" {
		t.Fatalf("rows = %#v", result.Rows)
	}
}

func TestUnionAndJoinTables(t *testing.T) {
	store, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := ingest.New(store)
	ual := `{"ts":"2024-01-01T00:00:00Z","user":"alice","host":"cloud"}
{"ts":"2024-01-01T00:01:00Z","user":"bob","host":"cloud"}`
	if _, err := service.Import(t.Context(), strings.NewReader(ual), false, ingest.Mapping{
		Name: "ual", Table: "UAL", Source: "ual", TimestampPath: "ts",
		FieldPaths: map[string]string{"User": "user", "Host": "host"},
	}); err != nil {
		t.Fatal(err)
	}
	sysmon := `{"ts":"2024-01-01T00:02:00Z","user":"alice","host":"endpoint","message":"powershell"}
{"ts":"2024-01-01T00:03:00Z","user":"charlie","host":"endpoint","message":"cmd"}`
	if _, err := service.Import(t.Context(), strings.NewReader(sysmon), false, ingest.Mapping{
		Name: "sysmon", Table: "Sysmon", Source: "sysmon", TimestampPath: "ts",
		FieldPaths: map[string]string{"User": "user", "Host": "host", "Message": "message"},
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()

	union := `UAL | project User, Host | union (Sysmon | project Host, User)`
	unionRows := queryRows(t, server.URL, union)
	if len(unionRows) != 4 {
		t.Fatalf("union rows = %#v", unionRows)
	}

	inner := `UAL | project User, Host | join (Sysmon | project User, Host, Message) on User`
	innerRows := queryRows(t, server.URL, inner)
	if len(innerRows) != 1 || innerRows[0]["User"] != "alice" || innerRows[0]["Host"] != "cloud" || innerRows[0]["Host1"] != "endpoint" || innerRows[0]["Message"] != "powershell" {
		t.Fatalf("inner join rows = %#v", innerRows)
	}

	left := `UAL | project User, Host | join kind=leftouter (Sysmon | project User, Message) on User | order by User`
	leftRows := queryRows(t, server.URL, left)
	if len(leftRows) != 2 || leftRows[1]["User"] != "bob" || leftRows[1]["Message"] != nil {
		t.Fatalf("left join rows = %#v", leftRows)
	}
}

func queryRows(t *testing.T, serverURL, query string) []map[string]any {
	t.Helper()
	response, err := http.Post(serverURL+"/api/query", "application/json", bytes.NewBuffer(mustJSON(t, map[string]string{"query": query})))
	if err != nil {
		t.Fatal(err)
	}
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
