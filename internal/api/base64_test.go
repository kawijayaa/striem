package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestBase64ThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(text:string,bytes:dynamic);print encoded=base64_encode_tostring(text),decoded=base64_decode_toarray("AP+A"),fromarray=base64_encode_fromarray(bytes),invalid=base64_decode_tostring("AP+A"),empty=base64_encode_fromarray(dynamic([]))`,
		"parameters": map[string]any{"text": "Kusto", "bytes": []int{0, 255, 128}},
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
	for key, want := range map[string]string{"encoded": `"S3VzdG8="`, "decoded": "[0,255,128]", "fromarray": `"AP+A"`, "invalid": "null", "empty": `""`} {
		if got := string(result.Rows[0][key]); got != want {
			t.Fatalf("%s got %s want %s", key, got, want)
		}
	}
}
