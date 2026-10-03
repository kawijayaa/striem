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

const nativeProgressiveResponse = `[
 {"FrameType":"DataSetHeader","Version":"v2.0","IsProgressive":true},
 {"FrameType":"TableHeader","TableId":2,"TableKind":"PrimaryResult","TableName":"Results","Columns":[{"ColumnName":"Value","ColumnType":"long"}]},
 {"FrameType":"TableFragment","TableId":2,"FieldCount":1,"TableFragmentType":"DataAppend","Rows":[[1],[2]]},
 {"FrameType":"TableFragment","TableId":2,"FieldCount":1,"TableFragmentType":"DataReplace","Rows":[[9223372036854775807]]},
 {"FrameType":"TableProgress","TableId":2,"TableProgress":99.5},
 {"FrameType":"TableCompletion","TableId":2,"RowCount":1},
 {"FrameType":"DataSetCompletion","HasErrors":false,"Cancelled":false}
]`

func TestNativeProgressiveAPI(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       int
	}{
		{"success", nativeProgressiveResponse, 200},
		{"partial", strings.Replace(nativeProgressiveResponse, `"HasErrors":false`, `"HasErrors":true`, 1), 502},
		{"incomplete", strings.Replace(nativeProgressiveResponse, `{"FrameType":"TableCompletion","TableId":2,"RowCount":1},`, ``, 1), 502},
		{"wrong-count", strings.Replace(nativeProgressiveResponse, `"RowCount":1`, `"RowCount":3`, 1), 502},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var request struct {
					Properties string `json:"properties"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				var properties struct{ Options map[string]any }
				if err := json.Unmarshal([]byte(request.Properties), &properties); err != nil || properties.Options["request_readonly"] != true || properties.Options["results_progressive_enabled"] != true {
					t.Errorf("missing native options: %s %v", request.Properties, err)
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, test.body)
			}))
			defer service.Close()
			client, err := kusto.New(kusto.Config{Endpoint: service.URL, HTTPClient: service.Client(), TokenProvider: func(context.Context) (string, error) { return "server-secret", nil }})
			if err != nil {
				t.Fatal(err)
			}
			apiServer := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err := apiServer.SetNativeKusto(NativeKustoConfig{Client: client, Database: "Native", AllowManagement: true}); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(apiServer.Handler())
			defer server.Close()
			response := nativePost(t, server.URL, "/api/kusto/query", map[string]any{"query": "print x=1", "progressive": true})
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != test.want || strings.Contains(string(body), "server-secret") {
				t.Fatalf("bad native response: %d %s", response.StatusCode, body)
			}
			if test.want == 200 {
				var result struct {
					Tables      []nativeTable
					Progressive bool
				}
				if err := json.Unmarshal(body, &result); err != nil || !result.Progressive || len(result.Tables) != 1 || len(result.Tables[0].Rows) != 1 || string(result.Tables[0].Rows[0][0]) != "9223372036854775807" || result.Tables[0].Progress == nil || *result.Tables[0].Progress != 99.5 {
					t.Fatalf("bad final progressive result: %s %v", body, err)
				}
			} else if strings.Contains(string(body), `"tables"`) {
				t.Fatal("failed progressive result leaked tables")
			}
			for _, path := range []string{"/api/kusto/query", "/api/kusto/management"} {
				value := any("true")
				if path == "/api/kusto/management" {
					value = true
				}
				response := nativePost(t, server.URL, path, map[string]any{"query": "print x=1", "progressive": value})
				response.Body.Close()
				if response.StatusCode != 400 {
					t.Fatalf("invalid progressive request status: %d", response.StatusCode)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("invalid request reached native service")
			}
		})
	}
}
