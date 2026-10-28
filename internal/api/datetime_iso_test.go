package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestISOMinuteDatetimeThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(text:string);print converted=todatetime(text),literal=datetime(2017-10-15 12:00),invalid=todatetime("9999-12-31T23:59-01:00")`,
		"parameters": map[string]any{"text": "2024-01-01T02:30+02:30"},
	})
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("query: %d %s %v", response.StatusCode, body, err)
	}
	var result struct{ Rows []map[string]*string }
	if err := json.Unmarshal(body, &result); err != nil || len(result.Rows) != 1 {
		t.Fatalf("response: %s %v", body, err)
	}
	for key, want := range map[string]string{"converted": "2024-01-01T00:00:00.000000000Z", "literal": "2017-10-15T12:00:00.000000000Z"} {
		if got := result.Rows[0][key]; got == nil || *got != want {
			t.Fatalf("%s got %v want %s", key, got, want)
		}
	}
	if result.Rows[0]["invalid"] != nil {
		t.Fatal("out-of-domain UTC conversion lost null")
	}
}
