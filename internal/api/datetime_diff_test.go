package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestDatetimeDiffThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(left:datetime,right:datetime);print months=datetime_diff("month",left,right),days=datetime_diff("day",left,right),wide=datetime_diff("microsecond",datetime(9999-12-31T23:59:59.9999999Z),datetime(0001-01-01)),invalid=datetime_diff("bad",left,right)`,
		"parameters": map[string]any{"left": "2024-02-01T02:00:00+02:00", "right": "2024-01-31T23:59:59.9999999Z"},
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
	for key, want := range map[string]string{"months": "1", "days": "1", "wide": "\"315537897599999999\"", "invalid": "null"} {
		if got := string(result.Rows[0][key]); got != want {
			t.Fatalf("%s got %s want %s", key, got, want)
		}
	}
}
