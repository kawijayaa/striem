package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestUnicodeCodepointsThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(text:string,points:dynamic);print a=unicode_codepoints_from_string(text),s=unicode_codepoints_to_string(points,33),empty=make_string(dynamic([])),invalid=make_string(55296)`,
		"parameters": map[string]any{"text": "é😀\x00", "points": []int{75, 128512}},
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
	for key, want := range map[string]string{"a": "[233,128512,0]", "s": `"K😀!"`, "empty": `""`, "invalid": "null"} {
		if got := string(result.Rows[0][key]); got != want {
			t.Fatalf("%s got %s want %s", key, got, want)
		}
	}
}
