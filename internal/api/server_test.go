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
	"slices"
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
	if err := store.SetChallengeName(t.Context(), "Operation Northstar"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()

	queryBody := bytes.NewBufferString(`{"query":"Sysmon | extend Process=tostring(RawData.process.name) | where Process contains 'powershell' | project Host, Process"}`)
	response, err := postAPI(server.URL+"/api/query", queryBody)
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
		ChallengeName string   `json:"challengeName"`
		Operators     []string `json:"operators"`
		Functions     []string `json:"functions"`
		Tables        []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			EventCount  int64  `json:"eventCount"`
		} `json:"tables"`
	}
	if err := json.NewDecoder(schemaResponse.Body).Decode(&schema); err != nil {
		t.Fatal(err)
	}
	if schema.ChallengeName != "Operation Northstar" {
		t.Fatalf("challenge name = %q, want Operation Northstar", schema.ChallengeName)
	}
	if len(schema.Tables) != 2 || schema.Tables[0].Name != "Events" || schema.Tables[1].Name != "Sysmon" {
		t.Fatalf("schema tables = %#v", schema.Tables)
	}
	if schema.Tables[0].EventCount != 2 || schema.Tables[0].Description != "All datasets" || schema.Tables[1].EventCount != 2 || schema.Tables[1].Description != "demo" {
		t.Fatalf("schema table metadata = %#v", schema.Tables)
	}
	for _, operator := range []string{"parse", "parse-where", "parse-kv", "project-away", "project-rename", "project-reorder", "evaluate bag_unpack", "lookup"} {
		if !slices.Contains(schema.Operators, operator) {
			t.Fatalf("schema operators do not include %q: %v", operator, schema.Operators)
		}
	}
	for _, function := range []string{"array_length", "bag_keys", "bag_has_key", "set_has_element", "base64_decode_tostring", "url_decode", "ipv4_is_private", "ipv4_is_in_range", "isempty", "isnotempty", "todatetime"} {
		if !slices.Contains(schema.Functions, function) {
			t.Fatalf("schema functions do not include %q: %v", function, schema.Functions)
		}
	}
}

func TestStateChangingRequestsRequireSameOrigin(t *testing.T) {
	store, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()

	tests := []struct {
		name   string
		header map[string]string
		status int
	}{
		{name: "missing verification header", header: map[string]string{"Content-Type": "application/json"}, status: http.StatusForbidden},
		{name: "cross-site fetch", header: map[string]string{"Content-Type": "application/json", "X-Striem-Request": "1", "Sec-Fetch-Site": "cross-site"}, status: http.StatusForbidden},
		{name: "foreign origin", header: map[string]string{"Content-Type": "application/json", "X-Striem-Request": "1", "Origin": "https://attacker.example"}, status: http.StatusForbidden},
		{name: "same origin", header: map[string]string{"Content-Type": "application/json", "X-Striem-Request": "1", "Origin": server.URL, "Sec-Fetch-Site": "same-origin"}, status: http.StatusOK},
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

func TestQueryValidation(t *testing.T) {
	store, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()

	t.Run("valid query", func(t *testing.T) {
		response, err := postAPI(server.URL+"/api/query/validate", strings.NewReader(`{"query":"Events | take 1"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(response.Body)
			t.Fatalf("status = %d, want %d: %s", response.StatusCode, http.StatusNoContent, body)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("Cache-Control = %q, want no-store", response.Header.Get("Cache-Control"))
		}
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if len(body) != 0 {
			t.Fatalf("body = %q, want empty response without rows or SQL", body)
		}
	})

	for _, test := range []struct {
		name  string
		query string
	}{
		{name: "parser error", query: "Events | where"},
		{name: "compiler error", query: "Events | where Missing == 1"},
	} {
		t.Run(test.name+" includes position", func(t *testing.T) {
			response, err := postAPI(server.URL+"/api/query/validate", bytes.NewReader(mustJSON(t, map[string]string{"query": test.query})))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				body, _ := io.ReadAll(response.Body)
				t.Fatalf("status = %d, want %d: %s", response.StatusCode, http.StatusBadRequest, body)
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
			if result.Error == "" || result.Position.Line < 1 || result.Position.Column < 1 {
				t.Fatalf("diagnostic = %#v, want error with positive line and column", result)
			}
		})
	}

	t.Run("requires same-origin header", func(t *testing.T) {
		request, err := http.NewRequest(http.MethodPost, server.URL+"/api/query/validate", strings.NewReader(`{"query":"Events | take 1"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusForbidden)
		}
	})

	t.Run("rejects wrong media type", func(t *testing.T) {
		request, err := http.NewRequest(http.MethodPost, server.URL+"/api/query/validate", strings.NewReader(`{"query":"Events | take 1"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "text/plain")
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
	manifestPath := filepath.Join(fixtureDirectory, "datasets.yaml")
	manifest := `challengeName: Northstar Investigation
datasets:
  - name: Northstar Microsoft 365 audit logs
    table: UAL
    path: events.csv
    format: csv
    source: microsoft365
    timestampPath: CreationDate
    timestampFormat: 2/01/2006 3:04:05 PM
    fieldPaths:
      EventType: Operations
      User: UserIds
      Message: RecordType
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
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
	response, err := postAPI(server.URL+"/api/query", bytes.NewBuffer(mustJSON(t, map[string]string{"query": query})))
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

	response, err := postAPI(server.URL+"/api/query", bytes.NewBufferString(`{"query":"Events | extend Host='new' | where Host == 'new' | project Host"}`))
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
	response, err := postAPI(server.URL+"/api/query", bytes.NewBuffer(mustJSON(t, map[string]string{"query": query})))
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

func TestSecurityKQLFeaturesExecute(t *testing.T) {
	store, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.DB().Exec(`INSERT INTO datasets(id,name,table_name,source,timestamp_path,created_at,event_count) VALUES(1,'x','Test','x','ts','2024-01-01T00:00:00Z',2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO events(dataset_id,time_generated,source,host,username,message,raw_data) VALUES
		(1,'2024-01-01T00:00:00.000000000Z','sysmon','pc-1','Admin','PowerShell.exe alpha-user','{"tags":["Alpha","Beta"]}'),
		(1,'2024-01-01T00:01:00.000000000Z','sysmon','pc-2','guest','notpowershell gamma-user','{"tags":["Gamma"]}')`); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()

	indexed := queryRows(t, server.URL, `Events | where Message has_all ("powershell", "alpha") and Message has_any ("cmd", "powershell") and User =~ "ADMIN" and User !~ "guest" and User !in~ ("GUEST") | project Tag=tostring(RawData.tags[0])`)
	if len(indexed) != 1 || indexed[0]["Tag"] != "Alpha" {
		t.Fatalf("indexed rows = %#v", indexed)
	}
	expanded := queryRows(t, server.URL, `Events | extend Tags=RawData.tags | mv-expand Tag=Tags limit 8 | where Tag in~ ("beta", "gamma") | project Host, Tag | order by Host`)
	if len(expanded) != 2 || expanded[0]["Tag"] != "Beta" || expanded[1]["Tag"] != "Gamma" {
		t.Fatalf("expanded rows = %#v", expanded)
	}
	applied := queryRows(t, server.URL, `Events | mv-apply Tag=RawData.tags on (where Tag in~ ("beta", "gamma") | summarize MatchCount=count(), Matches=make_list(Tag)) | project Host, MatchCount, Matches | order by Host`)
	if len(applied) != 2 || applied[0]["MatchCount"] != float64(1) || applied[1]["MatchCount"] != float64(1) {
		t.Fatalf("applied rows = %#v", applied)
	}
	if matches, ok := applied[0]["Matches"].([]any); !ok || len(matches) != 1 || matches[0] != "Beta" {
		t.Fatalf("applied matches = %#v", applied[0]["Matches"])
	}
	missing := queryRows(t, server.URL, `Events | mv-apply Tag=RawData.tags on (where Tag == "missing" | summarize MatchCount=count()) | project Host, MatchCount | order by Host`)
	if len(missing) != 2 || missing[0]["MatchCount"] != float64(0) || missing[1]["MatchCount"] != float64(0) {
		t.Fatalf("empty applied rows = %#v", missing)
	}
	dynamicStrings := queryRows(t, server.URL, `Events | take 1 | mv-apply Item=parse_json('["true","1","null",{"x":1}]') on (summarize Values=make_list(Item)) | project Values`)
	values, ok := dynamicStrings[0]["Values"].([]any)
	if !ok || len(values) != 4 || values[0] != "true" || values[1] != "1" || values[2] != "null" {
		t.Fatalf("dynamic string values = %#v", dynamicStrings)
	}
	mixed := queryRows(t, server.URL, `Events | take 1 | mv-apply Item=parse_json('[{"x":1},"plain"]') on (summarize Values=make_list(Item.x)) | project Values`)
	mixedValues, ok := mixed[0]["Values"].([]any)
	if !ok || len(mixedValues) != 1 || mixedValues[0] != float64(1) {
		t.Fatalf("mixed dynamic values = %#v", mixed)
	}
	parsed := queryRows(t, server.URL, `Events | where Host == "pc-1" | extend Parts=split(Message, " ") | mv-expand Part=Parts | where Part contains_cs "PowerShell" | project Part, UserPart=extract("([a-z]+)-user", 1, Message), Clean=trim("\\s+", "  value  "), Replaced=replace_string(Message, "PowerShell", "pwsh")`)
	if len(parsed) != 1 || parsed[0]["Part"] != "PowerShell.exe" || parsed[0]["UserPart"] != "alpha" || parsed[0]["Clean"] != "value" || parsed[0]["Replaced"] != "pwsh.exe alpha-user" {
		t.Fatalf("parsed rows = %#v", parsed)
	}
}

func TestDataShapingKQLFeaturesExecute(t *testing.T) {
	store, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.DB().Exec(`INSERT INTO datasets(id,name,table_name,source,timestamp_path,created_at,event_count) VALUES(1,'x','Test','x','ts','2024-01-01T00:00:00Z',2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO events(dataset_id,time_generated,source,host,username,message,raw_data) VALUES
		(1,'2024-01-01T00:00:00.000000000Z','sysmon','pc-1','','user=alice id=42','{"command":"pwsh","score":7,"context":{"admin":true},"items":[1,2]}'),
		(1,'2024-01-01T00:01:00.000000000Z','sysmon','pc-2',NULL,'invalid','{"command":5,"score":"bad","context":"true","items":{}}')`); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()

	parsed := queryRows(t, server.URL, `Events | parse Message with "user=" ParsedUser:string " id=" ParsedID:long | project Host, ParsedUser, ParsedID | order by Host`)
	if len(parsed) != 2 || parsed[0]["ParsedUser"] != "alice" || parsed[0]["ParsedID"] != float64(42) || parsed[1]["ParsedUser"] != nil {
		t.Fatalf("parsed rows = %#v", parsed)
	}
	parsedWhere := queryRows(t, server.URL, `Events | parse-where Message with "user=" ParsedUser:string " id=" ParsedID:long | project ParsedUser, ParsedID`)
	if len(parsedWhere) != 1 || parsedWhere[0]["ParsedUser"] != "alice" || parsedWhere[0]["ParsedID"] != float64(42) {
		t.Fatalf("parse-where rows = %#v", parsedWhere)
	}
	parsedKV := queryRows(t, server.URL, `Events | take 1 | parse-kv "ParsedUser=alice ParsedID=42" as (ParsedUser:string, ParsedID:long) with (pair_delimiter=" ", kv_delimiter="=") | project ParsedUser, ParsedID`)
	if len(parsedKV) != 1 || parsedKV[0]["ParsedUser"] != "alice" || parsedKV[0]["ParsedID"] != float64(42) {
		t.Fatalf("parse-kv rows = %#v", parsedKV)
	}
	indexed := queryRows(t, server.URL, `Events | where Host == "pc-1" | mv-expand with_itemindex=ItemIndex Item=RawData.items | project Item, ItemIndex | order by ItemIndex`)
	if len(indexed) != 2 || indexed[0]["Item"] != float64(1) || indexed[0]["ItemIndex"] != float64(0) || indexed[1]["Item"] != float64(2) || indexed[1]["ItemIndex"] != float64(1) {
		t.Fatalf("indexed expansion rows = %#v", indexed)
	}
	dynamicString := queryRows(t, server.URL, `Events | take 1 | mv-expand Item=parse_json('["true"]') | where Item == "true" | project Item`)
	if len(dynamicString) != 1 || dynamicString[0]["Item"] != "true" {
		t.Fatalf("dynamic string expansion rows = %#v", dynamicString)
	}
	projected := queryRows(t, server.URL, `Events | project-away Source, EventType, RawData | project-rename Computer=Host, Account=User | project Computer, Account | order by Computer`)
	if len(projected) != 2 || projected[0]["Computer"] != "pc-1" {
		t.Fatalf("projected rows = %#v", projected)
	}
	unpacked := queryRows(t, server.URL, `Events | evaluate bag_unpack(RawData) : (*, command:string, score:long, context:dynamic, items:dynamic) | project Host, command, score, context, items | order by Host`)
	if len(unpacked) != 2 || unpacked[0]["command"] != "pwsh" || unpacked[0]["score"] != float64(7) || unpacked[1]["command"] != nil || unpacked[1]["score"] != nil {
		t.Fatalf("unpacked rows = %#v", unpacked)
	}
	if context, ok := unpacked[0]["context"].(map[string]any); !ok || context["admin"] != true || unpacked[1]["context"] != "true" {
		t.Fatalf("unpacked dynamic values = %#v", unpacked)
	}
	helpers := queryRows(t, server.URL, `Events | extend Size=array_length(RawData.items), Keys=bag_keys(RawData), Empty=isempty(User), Present=isnotempty(Message), Parsed=todatetime("2024-01-01T01:00:00+01:00") | project Host, Size, Keys, Empty, Present, Parsed | order by Host`)
	if len(helpers) != 2 || helpers[0]["Size"] != float64(2) || helpers[1]["Size"] != nil || helpers[0]["Empty"] != float64(1) || helpers[0]["Parsed"] != "2024-01-01T00:00:00.000000000Z" {
		t.Fatalf("helper rows = %#v", helpers)
	}
	if keys, ok := helpers[0]["Keys"].([]any); !ok || len(keys) != 4 {
		t.Fatalf("bag keys = %#v", helpers[0]["Keys"])
	}
	securityHelpers := queryRows(t, server.URL, `Events | take 1 | extend Decoded=base64_decode_tostring("cG93ZXJzaGVsbA=="), URL=url_decode("a%2Fb+c"), Has=bag_has_key(RawData, "$.context.admin"), Member=set_has_element(RawData.items, 2), Private=ipv4_is_private("10.1.2.3"), InRange=ipv4_is_in_range("192.168.1.2", "10.0.0.0/8,192.168.1.2") | project Decoded, URL, Has, Member, Private, InRange`)
	if len(securityHelpers) != 1 || securityHelpers[0]["Decoded"] != "powershell" || securityHelpers[0]["URL"] != "a/b+c" || securityHelpers[0]["Has"] != float64(1) || securityHelpers[0]["Member"] != float64(1) || securityHelpers[0]["Private"] != float64(1) || securityHelpers[0]["InRange"] != float64(1) {
		t.Fatalf("security helper rows = %#v", securityHelpers)
	}
}

func TestPriorityInvestigationKQLFeaturesExecute(t *testing.T) {
	store, err := database.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.DB().Exec(`INSERT INTO datasets(id,name,table_name,source,timestamp_path,created_at,event_count) VALUES(1,'x','Test','x','ts','2024-01-01T00:00:00Z',3)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO events(dataset_id,time_generated,source,host,username,message,raw_data) VALUES
		(1,'2024-01-01T00:00:00.000000000Z','sysmon','pc-1','alice','started','{"command":"PowerShell.exe","score":1}'),
		(1,'2024-01-01T00:01:00.000000000Z','sysmon','pc-1','bob','completed','{"command":"PowerShell.exe","score":2}'),
		(1,'2024-01-01T00:02:00.000000000Z','sysmon','pc-2','alice','benign','{"command":"cmd.exe","score":3}')`); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()

	latest := queryRows(t, server.URL, `let hits = Events | search "powershell";
hits | summarize arg_max(TimeGenerated, *) by Host`)
	if len(latest) != 1 || latest[0]["Host"] != "pc-1" || latest[0]["User"] != "bob" {
		t.Fatalf("latest rows = %#v", latest)
	}
	if raw, ok := latest[0]["RawData"].(map[string]any); !ok || raw["score"] != float64(2) {
		t.Fatalf("latest RawData = %#v", latest[0]["RawData"])
	}
	collision := queryRows(t, server.URL, `Events | where Host == "pc-1" | extend __kql_rank=0, __kql_group_0="user" | summarize arg_max(TimeGenerated, *) by Host`)
	if len(collision) != 1 || collision[0]["User"] != "bob" {
		t.Fatalf("collision rows = %#v", collision)
	}

	aggregated := queryRows(t, server.URL, `Events | where Host == "pc-1" | summarize Users=make_set(User), Sequence=make_list(User), Objects=make_list(RawData), Sample=take_any(User)`)
	if len(aggregated) != 1 {
		t.Fatalf("aggregate rows = %#v", aggregated)
	}
	users, usersOK := aggregated[0]["Users"].([]any)
	sequence, sequenceOK := aggregated[0]["Sequence"].([]any)
	objects, objectsOK := aggregated[0]["Objects"].([]any)
	objectOK := false
	if objectsOK && len(objects) > 0 {
		_, objectOK = objects[0].(map[string]any)
	}
	if !usersOK || len(users) != 2 || !sequenceOK || len(sequence) != 2 || !objectsOK || len(objects) != 2 || !objectOK || aggregated[0]["Sample"] == nil {
		t.Fatalf("aggregate row = %#v", aggregated[0])
	}
	projected := queryRows(t, server.URL, `Events | where Host == "pc-1" | project Payload=RawData`)
	if _, ok := projected[0]["Payload"].(map[string]any); !ok {
		t.Fatalf("projected dynamic value = %#v", projected[0]["Payload"])
	}
	joined := queryRows(t, server.URL, `let left = Events | where Host == "pc-1" | project User, Host;
let right = Events | where Message == "benign" | project User, Message;
left | join (right) on User`)
	if len(joined) != 1 || joined[0]["User"] != "alice" || joined[0]["Message"] != "benign" {
		t.Fatalf("tabular join rows = %#v", joined)
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

	anti := `UAL | project User, Host | join kind=leftanti (Sysmon | project User) on User | order by User`
	antiRows := queryRows(t, server.URL, anti)
	if len(antiRows) != 1 || antiRows[0]["User"] != "bob" {
		t.Fatalf("leftanti join rows = %#v", antiRows)
	}

	lookup := `UAL | project User, Host | lookup (Sysmon | project User, Message) on User | order by User`
	lookupRows := queryRows(t, server.URL, lookup)
	if len(lookupRows) != 2 || lookupRows[0]["User"] != "alice" || lookupRows[0]["Message"] != "powershell" || lookupRows[1]["User"] != "bob" || lookupRows[1]["Message"] != nil {
		t.Fatalf("lookup rows = %#v", lookupRows)
	}
}

func queryRows(t *testing.T, serverURL, query string) []map[string]any {
	t.Helper()
	response, err := postAPI(serverURL+"/api/query", bytes.NewBuffer(mustJSON(t, map[string]string{"query": query})))
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

func postAPI(url string, body io.Reader) (*http.Response, error) {
	request, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Striem-Request", "1")
	return http.DefaultClient.Do(request)
}
