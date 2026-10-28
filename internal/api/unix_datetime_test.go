package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestUnixDatetimeThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(nanos:long);print date=unixtime_nanoseconds_todatetime(nanos),year=getyear(unixtime_nanoseconds_todatetime(nanos)),invalid=unixtime_seconds_todatetime(long(null))`,
		"parameters": map[string]any{"nanos": json.Number("1546300800123456700")},
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
	for key, want := range map[string]string{"date": `"2019-01-01T00:00:00.123456700Z"`, "year": "2019", "invalid": "null"} {
		if got := string(result.Rows[0][key]); got != want {
			t.Fatalf("%s got %s want %s", key, got, want)
		}
	}
}
