package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestParseOperatorThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(line:string);print Message=line | parse-where Message with "user=" User " port=" Port:long " at=" At:datetime | project User, Port, At`,
		"parameters": map[string]any{"line": "user=root'); -- port=9007199254740993 at=08-Nov-14 15:05:02 GMT"},
	})
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("query: %d %s %v", response.StatusCode, body, err)
	}
	var result struct{ Rows []map[string]json.RawMessage }
	if err := json.Unmarshal(body, &result); err != nil || len(result.Rows) != 1 {
		t.Fatalf("response: %s %v", body, err)
	}
	for key, want := range map[string]string{"User": `"root'); --"`, "Port": "\"9007199254740993\"", "At": `"2014-11-08T15:05:02.000000000Z"`} {
		if got := string(result.Rows[0][key]); got != want {
			t.Fatalf("%s got %s want %s; body=%s", key, got, want, body)
		}
	}
}
