package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestHashesThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(text:string,date:datetime);print md5=hash_md5(text),sha1=hash_sha1(text),sha256=hash_sha256(text),dated=hash_sha256(date)`,
		"parameters": map[string]any{"text": "World", "date": "2020-01-01T02:00:00+02:00"},
	})
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("query: %d %s %v", response.StatusCode, body, err)
	}
	var result struct{ Rows []map[string]string }
	if err := json.Unmarshal(body, &result); err != nil || len(result.Rows) != 1 {
		t.Fatalf("response: %s %v", body, err)
	}
	for key, want := range map[string]string{
		"md5": "f5a7924e621e84c9280a9a27e1bcb7f6", "sha1": "70c07ec18ef89c5309bbb0937f3a6342411e1fdd", "sha256": "78ae647dc5544d227130a0682a51e30bc7777fbb6d8a8f17007463a3ecd1d524", "dated": "ba666752dc1a20eb750b0eb64e780cc4c968bc9fb8813461c1d7e750f302d71d",
	} {
		if got := result.Rows[0][key]; got != want {
			t.Fatalf("%s got %s want %s", key, got, want)
		}
	}
}
