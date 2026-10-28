package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalQueryParametersThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	query := `declare query_parameters(n:long,flag:bool,bag:dynamic,text:string,when:datetime);print n=n,flag=flag,bag=bag,text=text,when=when`
	parameters := map[string]any{"n": json.Number("9223372036854775807"), "flag": true, "bag": json.RawMessage(`{"n":9007199254740993,"flag":false}`), "text": "' OR 1=1; --\x00end", "when": "2026-01-02T01:00:00+01:00"}
	response := localParameterPost(t, handler, "/api/query", map[string]any{"query": query, "parameters": parameters})
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("query failed: %d %s", response.StatusCode, body)
	}
	var result struct{ Rows []map[string]json.RawMessage }
	if err := json.Unmarshal(body, &result); err != nil || len(result.Rows) != 1 {
		t.Fatalf("invalid result: %s %v", body, err)
	}
	row := result.Rows[0]
	// Integers beyond JavaScript's safe range are sent as exact strings.
	if string(row["n"]) != `"9223372036854775807"` || string(row["flag"]) != "true" || string(row["when"]) != `"2026-01-02T00:00:00.000000000Z"` || !strings.Contains(string(row["bag"]), "9007199254740993") {
		t.Fatalf("typed values lost: %s", body)
	}
	var text string
	if err := json.Unmarshal(row["text"], &text); err != nil || text != parameters["text"] {
		t.Fatal("text parameter changed")
	}
	response = localParameterPost(t, handler, "/api/query/validate", map[string]any{"query": query, "parameters": parameters})
	response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatal("parameterized validation failed")
	}
	// Defaults also work through the ordinary editor's query-only request shape.
	response = localParameterPost(t, handler, "/api/query", map[string]any{"query": `declare query_parameters(n:long=3);datatable(x:long)[1,4,7]|where x>n`})
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || response.StatusCode != 200 || len(result.Rows) != 2 {
		t.Fatalf("defaults failed: status=%d, result=%v, error=%v", response.StatusCode, result, err)
	}
	response.Body.Close()
	// Date parameters reused in a shared let/function expression must not acquire
	// nested runtime conversions when the SQL expression graph is normalized.
	response = localParameterPost(t, handler, "/api/query", map[string]any{"query": `declare query_parameters(d:datetime);let f=(){iff(true,d,d)};print a=f(),b=f()`, "parameters": map[string]any{"d": "2026-01-02"}})
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || strings.Count(string(body), "2026-01-02T00:00:00.000000000Z") != 2 {
		t.Fatalf("reused date parameter changed: %s", body)
	}
}

func TestLocalQueryParameterErrors(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	for _, body := range []string{
		`{"query":"declare query_parameters(x:long);print x"}`,
		`{"query":"declare query_parameters(x:long);print x","parameters":{"x":"secret-value"}}`,
		`{"query":"declare query_parameters(x:int);print x","parameters":{"x":2147483648}}`,
		`{"query":"declare query_parameters(x:long);print x","parameters":{"x":9223372036854775808}}`,
		`{"query":"declare query_parameters(x:bool);print x","parameters":{"x":1}}`,
		`{"query":"declare query_parameters(x:datetime);print x","parameters":{"x":"secret-value"}}`,
		`{"query":"declare query_parameters(x:string);print x","parameters":{"x":null}}`,
		`{"query":"declare query_parameters(x:long);print x","parameters":{"x":1,"x":2}}`,
		`{"query":"print 1","parameters":[]}`,
		`{"query":"print 1","parameters":{"x":"` + "\xff" + `"}}`,
		`{"query":"print '` + "\xff" + `'"}`,
		`{"query":"print 1","parameters":{"unused":"` + strings.Repeat("x", 64<<10) + `"}}`,
		`{"query":"print 1","parameters":{"unused":1}}{}`,
	} {
		for _, path := range []string{"/api/query", "/api/query/validate"} {
			response := localParameterBody(handler, path, body)
			data, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != 400 || strings.Contains(string(data), "secret-value") {
				t.Fatalf("invalid parameter accepted/leaked: %d %s", response.StatusCode, data)
			}
		}
	}
}

func TestLocalQueryParameterEmptyAndNullValues(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(n:long=9,flag:bool=true,text:string="fallback",bag:dynamic);print n=n,flag=flag,text=text,bag=bag`,
		"parameters": map[string]any{"n": nil, "flag": false, "text": "", "bag": nil},
	})
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("nullable values failed: %d %s %v", response.StatusCode, body, err)
	}
	var result struct{ Rows []map[string]json.RawMessage }
	if err := json.Unmarshal(body, &result); err != nil || len(result.Rows) != 1 {
		t.Fatalf("invalid nullable result: %s %v", body, err)
	}
	for name, want := range map[string]string{"n": "null", "flag": "false", "text": `""`, "bag": "null"} {
		if string(result.Rows[0][name]) != want {
			t.Fatalf("%s: got %s want %s", name, result.Rows[0][name], want)
		}
	}
}

func TestLocalQueryParameterDeclarationBounds(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	declarations := func(start, count int) string {
		parts := make([]string, count)
		for i := range parts {
			parts[i] = fmt.Sprintf("p%d:long=1", start+i)
		}
		return "declare query_parameters(" + strings.Join(parts, ",") + ");"
	}
	for _, test := range []struct {
		query  string
		status int
	}{
		{declarations(0, 64) + "print x=p0", 200},
		{declarations(0, 65) + "print x=p0", 400},
	} {
		response := localParameterPost(t, handler, "/api/query", map[string]any{"query": test.query})
		response.Body.Close()
		if response.StatusCode != test.status {
			t.Fatalf("declaration width status=%d want=%d", response.StatusCode, test.status)
		}
	}
	var script strings.Builder
	for start := 0; start < 1024; start += 64 {
		script.WriteString(declarations(start, 64))
	}
	for _, extra := range []bool{false, true} {
		query := script.String()
		want := 200
		if extra {
			query += declarations(1024, 1)
			want = 400
		}
		response := localParameterPost(t, handler, "/api/query", map[string]any{"query": query + "print x=p0"})
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("total declaration bound status=%d want=%d", response.StatusCode, want)
		}
	}
	for _, count := range []int{1024, 1025} {
		values := make(map[string]any, count)
		for i := 0; i < count; i++ {
			values[fmt.Sprintf("p%d", i)] = 1
		}
		response := localParameterPost(t, handler, "/api/query", map[string]any{"query": "declare query_parameters(p0:long);print x=p0", "parameters": values})
		response.Body.Close()
		want := 200
		if count > 1024 {
			want = 400
		}
		if response.StatusCode != want {
			t.Fatalf("provided parameter count=%d status=%d want=%d", count, response.StatusCode, want)
		}
	}
}

func localParameterPost(t *testing.T, handler http.Handler, path string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return localParameterBody(handler, path, string(data))
}

func localParameterBody(handler http.Handler, path, body string) *http.Response {
	request := httptest.NewRequest(http.MethodPost, "http://striem.test"+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Striem-Request", "1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Result()
}
