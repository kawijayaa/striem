package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestIPAnyRangesThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(networks:dynamic,ip:string);print v4=ipv4_is_in_any_range(ip,networks),v6=ipv6_is_in_any_range(ip,networks),outside=ipv6_is_in_any_range(ip,"::1"),invalid=ipv4_is_in_any_range(ip,dynamic(["bad"]))`,
		"parameters": map[string]any{"ip": "192.168.1.6", "networks": []string{"127.0.0.0/8", "192.168.0.0/16"}},
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
	for key, want := range map[string]string{"v4": "true", "v6": "true", "outside": "false", "invalid": "null"} {
		if got := string(result.Rows[0][key]); got != want {
			t.Fatalf("%s got %s want %s", key, got, want)
		}
	}
}
