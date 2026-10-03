package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kawijayaa/ksql/kusto"
)

const nativeQueryResponse = `[
 {"FrameType":"DataSetHeader","Version":"v2.0","IsProgressive":false},
 {"FrameType":"DataTable","TableId":0,"TableKind":"PrimaryResult","TableName":"Results","Columns":[{"ColumnName":"Value","ColumnType":"long"}],"Rows":[[9223372036854775807]]},
 {"FrameType":"DataTable","TableId":1,"TableKind":"PrimaryResult","TableName":"Bags","Columns":[{"ColumnName":"Value","ColumnType":"dynamic"}],"Rows":[[{"nested":[true,null,9007199254740993]}]]},
 {"FrameType":"DataSetCompletion","HasErrors":false,"Cancelled":false}
]`

func TestNativeKustoAPIExecution(t *testing.T) {
	var calls atomic.Int32
	query := `externaldata(x:long)[h@"https://example.invalid/data.csv"] with (format="csv"); print bag=dynamic({"nested":[true,null]})`
	command := ".execute database script <|\n.create table Test (x:long)\n.show tables"
	service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer server-secret" {
			t.Error("missing server credentials")
		}
		var request struct {
			Database   string `json:"db"`
			Text       string `json:"csl"`
			Properties string `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if request.Database != "NativeDatabase" {
			t.Error("wrong native database")
		}
		var properties struct {
			Options    map[string]any
			Parameters map[string]string
		}
		if err := json.Unmarshal([]byte(request.Properties), &properties); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-ms-activity-id", "native-activity")
		switch r.URL.Path {
		case "/v2/rest/query":
			if request.Text != query || properties.Options["request_readonly"] != true || properties.Options["results_progressive_enabled"] != false || properties.Parameters["p"] != "';.drop table Test" {
				t.Errorf("query not forwarded safely: %+v %+v", request, properties)
			}
			io.WriteString(w, nativeQueryResponse)
		case "/v1/rest/mgmt":
			if request.Text != command || properties.Options["request_readonly"] != nil {
				t.Errorf("management not forwarded: %+v", request)
			}
			io.WriteString(w, `{"Tables":[{"TableName":"Result","Columns":[],"Rows":[]}]}`)
		default:
			t.Error("unexpected native path")
		}
	}))
	defer service.Close()
	client, err := kusto.New(kusto.Config{Endpoint: service.URL, HTTPClient: service.Client(), TokenProvider: func(context.Context) (string, error) { return "server-secret", nil }})
	if err != nil {
		t.Fatal(err)
	}
	apiServer := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	config := NativeKustoConfig{Client: client, Database: "NativeDatabase"}
	if err := apiServer.SetNativeKusto(config); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()
	response := nativePost(t, server.URL, "/api/kusto/query", map[string]any{"query": query, "parameters": map[string]string{"p": "';.drop table Test"}})
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(body), "9223372036854775807") || !strings.Contains(string(body), "9007199254740993") || strings.Contains(string(body), "server-secret") {
		t.Fatalf("invalid native response: %d %s", response.StatusCode, body)
	}
	var result struct {
		Tables     []nativeTable
		ActivityID string `json:"activityId"`
	}
	if err := json.Unmarshal(body, &result); err != nil || len(result.Tables) != 2 || result.Tables[1].Columns[0].Type != "dynamic" || result.ActivityID != "native-activity" {
		t.Fatalf("lost native types/tables: %s, %v", body, err)
	}
	response = nativePost(t, server.URL, "/api/kusto/management", map[string]any{"query": command})
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden || calls.Load() != 1 {
		t.Fatal("management executed without opt-in")
	}
	config.AllowManagement = true
	if err := apiServer.SetNativeKusto(config); err != nil {
		t.Fatal(err)
	}
	response = nativePost(t, server.URL, "/api/kusto/management", map[string]any{"query": command})
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(body), `"columns":[],"rows":[]`) || calls.Load() != 2 {
		t.Fatalf("management execution failed: %d %s", response.StatusCode, body)
	}
	// Native configuration leaves the ordinary editor's local query path usable.
	rows := queryRows(t, server.URL, "Events|count")
	if len(rows) != 1 || calls.Load() != 2 {
		t.Fatal("local queries sent to service")
	}
}

func TestNativeKustoAPIGuards(t *testing.T) {
	var calls atomic.Int32
	service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); t.Error("invalid request reached service") }))
	defer service.Close()
	client, err := kusto.New(kusto.Config{Endpoint: service.URL, HTTPClient: service.Client(), TokenProvider: func(context.Context) (string, error) { return "secret", nil }})
	if err != nil {
		t.Fatal(err)
	}
	apiServer := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(apiServer.Handler())
	defer server.Close()
	response := nativePost(t, server.URL, "/api/kusto/query", map[string]any{"query": "print x=1"})
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatal("unconfigured native endpoint enabled")
	}
	if err := apiServer.SetNativeKusto(NativeKustoConfig{Client: client}); err == nil {
		t.Fatal("accepted missing database")
	}
	if err := apiServer.SetNativeKusto(NativeKustoConfig{Client: client, Database: "Native"}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{}`, `{"query":""}`, `{"query":"print x=1","database":"Other"}`, `{"query":"print x=1","options":{"request_readonly":false}}`, `{"query":"print x=1","parameters":{"p":123}}`, `{"query":"print x=1"}{}`, `{"query":"` + strings.Repeat("x", 32769) + `"}`, `{"query":"\xff"}`, strings.Repeat(" ", 65537)} {
		request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/kusto/query", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Striem-Request", "1")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid native body accepted: %d", response.StatusCode)
		}
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/kusto/query", strings.NewReader(`{"query":"print x=1"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err = server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden || calls.Load() != 0 {
		t.Fatal("origin guards bypassed")
	}
}

func TestNativeKustoAPIFailureRedaction(t *testing.T) {
	for _, test := range []struct {
		name, body   string
		status, want int
	}{
		{"partial", strings.Replace(nativeQueryResponse, `"HasErrors":false`, `"HasErrors":true`, 1), 200, 502},
		{"semantic", `{"error":{"message":"secret-query-parameter"}}`, 400, 400},
		{"credential", `{"error":{"message":"secret-token"}}`, 401, 502},
		{"throttled", `{"error":{"message":"secret"}}`, 429, 429},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("x-ms-activity-id", "correlation")
				w.WriteHeader(test.status)
				io.WriteString(w, test.body)
			}))
			defer service.Close()
			client, err := kusto.New(kusto.Config{Endpoint: service.URL, HTTPClient: service.Client(), TokenProvider: func(context.Context) (string, error) { return "secret", nil }})
			if err != nil {
				t.Fatal(err)
			}
			apiServer := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err := apiServer.SetNativeKusto(NativeKustoConfig{Client: client, Database: "Samples"}); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(apiServer.Handler())
			defer server.Close()
			response := nativePost(t, server.URL, "/api/kusto/query", map[string]any{"query": "print x=1"})
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != test.want || strings.Contains(string(body), "secret") || strings.Contains(string(body), `"tables"`) || !strings.Contains(string(body), "correlation") || calls.Load() != 1 {
				t.Fatalf("bad failure response: %d %s", response.StatusCode, body)
			}
		})
	}
}

func nativePost(t *testing.T, base, path string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(string(data)))
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
