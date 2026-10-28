package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestBitwiseAggregatesThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(mask:long);datatable(v:long)[9223372036854775807,long(null)]|summarize a=binary_all_and(binary_and(v,mask)),o=binary_all_or(v),x=binary_all_xor(v),hex=tohex(binary_all_or(v))`,
		"parameters": map[string]any{"mask": json.Number("9223372036854775807")},
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
	for key, want := range map[string]string{"a": "9223372036854775807", "o": "9223372036854775807", "x": "9223372036854775807", "hex": `"7fffffffffffffff"`} {
		if got := string(result.Rows[0][key]); got != want {
			t.Fatalf("%s got %s want %s", key, got, want)
		}
	}
}
