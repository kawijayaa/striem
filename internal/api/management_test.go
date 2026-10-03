package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagementCatalogCommandsThroughAPI(t *testing.T) {
	store := testStore(t)
	apiServer := New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)
	rows := queryRows(t, server.URL, `.show tables`)
	if len(rows) != 1 || rows[0]["TableName"] != "Events" || rows[0]["DatabaseName"] != "striem" || rows[0]["Folder"] != "" || rows[0]["DocString"] != "" {
		t.Fatal(rows)
	}
	_, err := store.DB().Exec(`INSERT INTO datasets(id,name,table_name,source,timestamp_path,created_at) VALUES(1,'logs','Telemetry','fixture','ts','2026-01-01T00:00:00Z');
INSERT INTO dataset_fields(dataset_id,path,type) VALUES(1,'score','long'),(1,'host','string')`)
	if err != nil {
		t.Fatal(err)
	}
	// Catalog refresh publishes the same views used by normal event queries.
	if err := apiServer.RefreshCatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows = queryRows(t, server.URL, `.show tables | where TableName == "Telemetry"`)
	if len(rows) != 1 || rows[0]["TableName"] != "Telemetry" {
		t.Fatal(rows)
	}
	rows = queryRows(t, server.URL, `.show table Telemetry schema as json`)
	if len(rows) != 1 || len(rows[0]) != 5 {
		t.Fatal(rows)
	}
	text, ok := rows[0]["Schema"].(string)
	if !ok {
		t.Fatalf("schema must remain string: %#v", rows)
	}
	var schema struct {
		Name           string
		OrderedColumns []struct{ Name, Type, CslType string }
	}
	if err := json.Unmarshal([]byte(text), &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Name != "Telemetry" || len(schema.OrderedColumns) != 5 || schema.OrderedColumns[0].Name != "TimeGenerated" {
		t.Fatal(schema)
	}
	rows = queryRows(t, server.URL, `.show table Telemetry cslschema | project Schema`)
	if len(rows) != 1 || !strings.Contains(rows[0]["Schema"].(string), "score:long") {
		t.Fatal(rows)
	}
	response := queryAPIPath(t, server.URL+"/api/query/validate", `.show tables | count`)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatal(response.StatusCode)
	}
	for _, q := range []string{`.drop table Telemetry`, `.show tables details`, `.show table Missing cslschema`, `print toscalar(.show tables)`, `.show tables | project Missing`} {
		response := queryAPI(t, server.URL, q)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "position") {
			t.Fatalf("%s status=%d body=%s", q, response.StatusCode, body)
		}
	}
	if rows := queryRows(t, server.URL, `Telemetry|count`); len(rows) != 1 || rows[0]["Count"] != float64(0) {
		t.Fatal(rows)
	}
}
