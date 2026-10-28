package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIPv6FunctionsThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(ip:string,prefix:long);print parsed=parse_ipv6(ip),masked=parse_ipv6_mask(ip,prefix),comparison=ipv6_compare(ip,"::ffff:0.0.0.0"),matched=ipv6_is_match(ip,"::ffff:c0a8:01ff",prefix),different=ipv6_is_match(ip,"::1"),invalid=ipv6_is_match("bad",ip),empty=parse_ipv6("bad"),inside=ipv6_is_in_range(ip,"192.168.1.0/24"),outside=ipv6_is_in_range(ip,"::1"),invalidrange=ipv6_is_in_range(ip,"bad")`,
		"parameters": map[string]any{"ip": "192.168.1.1", "prefix": 120},
	})
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("IPv6 query: %d %s %v", response.StatusCode, body, err)
	}
	var result struct{ Rows []map[string]json.RawMessage }
	if err := json.Unmarshal(body, &result); err != nil || len(result.Rows) != 1 {
		t.Fatalf("invalid response: %s %v", body, err)
	}
	for name, want := range map[string]string{
		"inside": "true", "outside": "false", "invalidrange": "null",
		"parsed":     `"0000:0000:0000:0000:0000:ffff:c0a8:0101"`,
		"masked":     `"0000:0000:0000:0000:0000:ffff:c0a8:0100"`,
		"comparison": "1", "matched": "true", "different": "false", "invalid": "null", "empty": `""`,
	} {
		if got := string(result.Rows[0][name]); got != want {
			t.Fatalf("%s got %s want %s", name, got, want)
		}
	}
	for _, query := range []string{`print parse_ipv6(true)`, `print ipv6_compare("::1",2)`, `print ipv6_is_match("::1","::1",1.5)`} {
		response := localParameterPost(t, handler, "/api/query/validate", map[string]any{"query": query})
		response.Body.Close()
		if response.StatusCode != 400 {
			t.Fatal("invalid IPv6 signature accepted")
		}
	}
	request := httptest.NewRequest(http.MethodGet, "http://striem.test/api/schema", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var schema struct{ Functions []string }
	if err := json.Unmarshal(recorder.Body.Bytes(), &schema); err != nil || recorder.Code != 200 {
		t.Fatalf("discovery: %d %v", recorder.Code, err)
	}
	for _, name := range []string{"parse_ipv6", "parse_ipv6_mask", "ipv6_compare", "ipv6_is_match"} {
		found := false
		for _, actual := range schema.Functions {
			if actual == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing IPv6 discovery: %s", name)
		}
	}
}
