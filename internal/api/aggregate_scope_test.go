package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSummarizeScopeAndSingletonResultsThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	for _, q := range []string{
		`datatable(n:long)[1,2,3]|summarize x=7,b=true`,
		`datatable(n:long)[]|summarize x=7,b=true`,
		`datatable(n:long)[1,2,3]|summarize x=toscalar(datatable(v:long)[3,4]|summarize sum(v)),b=true`,
	} {
		rows := queryRows(t, server.URL, q)
		if len(rows) != 1 || len(rows[0]) != 2 || rows[0]["x"] != float64(7) || rows[0]["b"] != true {
			t.Fatalf("%s: %#v", q, rows)
		}
	}
	rows := queryRows(t, server.URL, `datatable(n:long,d:dynamic)[1,dynamic({"a":1}),3,dynamic({"b":3})]|summarize v=variance(n),distinct=count_distinct(n),bag=make_bag(d),list=make_list(n),set=make_set(n)`)
	if len(rows) != 1 || rows[0]["v"] != float64(2) || rows[0]["distinct"] != float64(2) {
		t.Fatal(rows)
	}
	bag, ok := rows[0]["bag"].(map[string]any)
	if !ok || bag["a"] != float64(1) || bag["b"] != float64(3) {
		t.Fatalf("bag metadata lost: %#v", rows)
	}
	for _, key := range []string{"list", "set"} {
		if values, ok := rows[0][key].([]any); !ok || len(values) != 2 {
			t.Fatalf("%s metadata lost: %#v", key, rows)
		}
	}
	for _, q := range []string{
		`print n=1|summarize n,count()`, `print n=1|summarize sum(n)+n`,
		`print n=1|summarize n by n`, `print n=1|summarize pack_array(*)`,
		`print n=1|summarize pack_array(n)`, `print n=1|summarize *,count()`,
		`print variance(1)`, `print make_bag(dynamic({}))`, `print make_set(1)`, `print make_list(1)`, `print dcount(1)`, `print count_distinct(1)`,
		`print n=1|summarize variance(count())`, `print n=1|summarize sum(variance(n))`,
		`print n=1|summarize make_set(count())`, `print n=1|summarize make_bag(make_list(n))`,
		`print n=1|summarize dcount(sum(n))`, `print n=1|summarize dcountif(n,1)`, `print n=1|summarize count_distinctif(n,1)`,
	} {
		response := queryAPI(t, server.URL, q)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "position") {
			t.Fatalf("%s status=%d body=%s", q, response.StatusCode, body)
		}
	}
}
