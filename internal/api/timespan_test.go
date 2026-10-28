package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestTimespanColumnsAreFormattedThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query": `print a=1d + 2h, b=-90s, c=1tick, d=timespan(null), e=datetime(2024-01-02) - datetime(2024-01-01 12:00), f=1d / 1h`,
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
	for key, want := range map[string]string{"a": `"1.02:00:00"`, "b": `"-00:01:30"`, "c": `"00:00:00.0000001"`, "d": "null", "e": `"12:00:00"`, "f": "24"} {
		if got := string(result.Rows[0][key]); got != want {
			t.Fatalf("%s got %s want %s; body=%s", key, got, want, body)
		}
	}
}
