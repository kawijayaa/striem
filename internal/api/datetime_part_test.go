package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestDatetimePartsThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(date:datetime);print year=getyear(date),month=getmonth(date),day=dayofmonth(date),ordinal=dayofyear(date),hour=hourofday(date),week=week_of_year(date),ns=datetime_part("nanosecond",date),quarter=datepart("quarter",date),invalid=getyear(datetime(null))`,
		"parameters": map[string]any{"date": "2024-02-29T14:30:59.1234567+02:00"},
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
	for key, want := range map[string]string{"year": "2024", "month": "2", "day": "29", "ordinal": "60", "hour": "12", "week": "9", "ns": "123456700", "quarter": "1", "invalid": "null"} {
		if got := string(result.Rows[0][key]); got != want {
			t.Fatalf("%s got %s want %s", key, got, want)
		}
	}
}
