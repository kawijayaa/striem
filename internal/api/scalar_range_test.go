package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestScalarRangeThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(first:long,last:long);print exact=range(first,last),real=range(0.5,1.5,0.5),empty=range(3,1),zero=range(1,3,0)`,
		"parameters": map[string]any{"first": json.Number("9223372036854775806"), "last": json.Number("9223372036854775807")},
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
	for key, want := range map[string]string{"exact": "[9223372036854775806,9223372036854775807]", "real": "[0.5,1,1.5]", "empty": "[]", "zero": "null"} {
		if got := string(result.Rows[0][key]); got != want {
			t.Fatalf("%s got %s want %s", key, got, want)
		}
	}
}
