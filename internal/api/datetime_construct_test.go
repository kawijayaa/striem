package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestDatetimeConstructionThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(date:datetime,amount:long);print added=datetime_add("month",amount,date),made=make_datetime(2024,2,29,12,30,0.1234567),invalid=make_datetime(2023,2,29),overflow=datetime_add("year",10000,date)`,
		"parameters": map[string]any{"date": "2024-01-31T14:00:00+02:00", "amount": 1},
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
	for key, want := range map[string]string{"added": "2024-02-29T12:00:00.000000000Z", "made": "2024-02-29T12:30:00.123456700Z"} {
		if got := result.Rows[0][key]; got == nil || *got != want {
			t.Fatalf("%s got %v want %s", key, got, want)
		}
	}
	if result.Rows[0]["invalid"] != nil || result.Rows[0]["overflow"] != nil {
		t.Fatal("invalid datetime lost null")
	}
}
