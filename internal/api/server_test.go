package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oka/striem/internal/database"
	"github.com/oka/striem/internal/deployment"
	"github.com/oka/striem/internal/ingest"
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
		Name: "demo", Source: "sysmon", TimestampPath: "ts", TimestampFormat: "auto",
		FieldPaths: map[string]string{"EventType": "kind", "Host": "host", "User": "user", "Message": "message"},
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()

	queryBody := bytes.NewBufferString(`{"query":"Events | extend Process=tostring(RawData.process.name) | where Process contains 'powershell' | project Host, Process"}`)
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
}

func TestMicrosoft365FixtureCanBeInvestigated(t *testing.T) {
	store, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manifestPath := filepath.Join("..", "..", "testdata", "datasets.json")
	loaded, err := deployment.Load(t.Context(), store, manifestPath)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if len(loaded) != 1 || loaded[0].EventCount != 120 {
		t.Fatalf("loaded datasets = %#v, want one with 120 events", loaded)
	}

	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()
	query := `Events
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
	if _, err := store.DB().Exec(`INSERT INTO datasets(id,name,source,timestamp_path,created_at,event_count) VALUES(1,'x','x','ts','2024-01-01T00:00:00Z',1)`); err != nil {
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
	if _, err := store.DB().Exec(`INSERT INTO datasets(id,name,source,timestamp_path,created_at,event_count) VALUES(1,'x','x','ts','2024-01-01T00:00:00Z',2)`); err != nil {
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
