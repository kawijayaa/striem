package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestBitwiseThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(value:long,shift:long);print a=binary_and(value,-1),o=binary_or(value,0),x=binary_xor(value,value),n=binary_not(value),l=binary_shift_left(1,shift),r=binary_shift_right(value,shift),c=bitset_count_ones(value),invalid=binary_shift_left(value,-1)`,
		"parameters": map[string]any{"value": json.Number("9223372036854775807"), "shift": 63},
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
	for key, want := range map[string]string{"a": "\"9223372036854775807\"", "o": "\"9223372036854775807\"", "x": "0", "n": "\"-9223372036854775808\"", "l": "\"-9223372036854775808\"", "r": "0", "c": "63", "invalid": "null"} {
		if got := string(result.Rows[0][key]); got != want {
			t.Fatalf("%s got %s want %s", key, got, want)
		}
	}
}
