package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIPv4FunctionsThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(ip:string,prefix:long);print n=parse_ipv4(ip),masked=parse_ipv4_mask(ip,prefix),formatted=format_ipv4(ip,prefix),cidr=format_ipv4_mask(ip,prefix),suffix=ipv4_netmask_suffix(ip),bad=parse_ipv4("bad"),empty=format_ipv4("bad"),comparison=ipv4_compare(ip,"0.0.0.0"),matched=ipv4_is_match(ip,"255.255.255.254",prefix),different=ipv4_is_match(ip,"0.0.0.0"),invalid=ipv4_is_match("bad",ip),ranges=ipv4_range_to_cidr_list("0.0.0.0",ip)`,
		"parameters": map[string]any{"ip": "255.255.255.255", "prefix": 31},
	})
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("IPv4 query: %d %s %v", response.StatusCode, body, err)
	}
	var result struct{ Rows []map[string]json.RawMessage }
	if err := json.Unmarshal(body, &result); err != nil || len(result.Rows) != 1 {
		t.Fatalf("bad IPv4 response: %s %v", body, err)
	}
	for name, want := range map[string]string{"n": "4294967295", "masked": "4294967294", "formatted": `"255.255.255.254"`, "cidr": `"255.255.255.254/31"`, "suffix": "32", "bad": "null", "empty": `""`, "comparison": "1", "matched": "true", "different": "false", "invalid": "null", "ranges": `["0.0.0.0/0"]`} {
		if got := string(result.Rows[0][name]); got != want {
			t.Fatalf("%s got %s want %s", name, got, want)
		}
	}
	for _, query := range []string{`print format_ipv4(true)`, `print parse_ipv4(1)`, `print parse_ipv4_mask("1.2.3.4",1.5)`, `print ipv4_compare(1,"1.2.3.4")`, `print ipv4_is_match("1.2.3.4","1.2.3.4",true)`} {
		response := localParameterPost(t, handler, "/api/query/validate", map[string]any{"query": query})
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 400 || !strings.Contains(string(body), "position") {
			t.Fatalf("invalid IPv4 signature accepted: %d %s", response.StatusCode, body)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "http://striem.test/api/schema", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var schema struct{ Functions []string }
	if err := json.Unmarshal(recorder.Body.Bytes(), &schema); err != nil || recorder.Code != 200 {
		t.Fatalf("discovery: %d %v", recorder.Code, err)
	}
	for _, name := range []string{"parse_ipv4", "parse_ipv4_mask", "format_ipv4", "format_ipv4_mask", "ipv4_netmask_suffix", "ipv4_compare", "ipv4_is_match", "ipv4_range_to_cidr_list"} {
		found := false
		for _, actual := range schema.Functions {
			if actual == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("discovery missing %s", name)
		}
	}
}
