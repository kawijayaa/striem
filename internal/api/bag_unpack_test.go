package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestBagUnpackThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query": `datatable(d:dynamic)[dynamic({"Name": "John", "Age":20, "Tags":["a"]}), dynamic({"Name": "Dave", "Height": 170, "Age":40})] | evaluate bag_unpack(d) | order by Age asc`,
	})
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("query: %d %s %v", response.StatusCode, body, err)
	}
	var result struct {
		Columns []string
		Rows    []map[string]json.RawMessage
	}
	if err := json.Unmarshal(body, &result); err != nil || len(result.Rows) != 2 {
		t.Fatalf("response: %s %v", body, err)
	}
	if got, _ := json.Marshal(result.Columns); string(got) != `["Age","Height","Name","Tags"]` {
		t.Fatalf("columns = %s", got)
	}
	if string(result.Rows[0]["Tags"]) != `["a"]` || string(result.Rows[1]["Height"]) != "170" || string(result.Rows[0]["Height"]) != "null" {
		t.Fatalf("rows = %s", body)
	}
}
