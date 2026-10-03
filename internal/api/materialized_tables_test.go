package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMaterializeTypedResultsAndDiagnosticsThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `let T=materialize(print d=dynamic({"x":[1,true,null]}),b=true); union T,T`)
	if len(rows) != 2 {
		t.Fatalf("rows=%#v", rows)
	}
	for _, row := range rows {
		if row["b"] != true {
			t.Fatalf("Boolean=%#v", row)
		}
		d, ok := row["d"].(map[string]any)
		if !ok {
			t.Fatalf("dynamic=%#v", row)
		}
		x, ok := d["x"].([]any)
		if !ok || len(x) != 3 || x[0] != float64(1) || x[1] != true || x[2] != nil {
			t.Fatalf("dynamic=%#v", d)
		}
	}
	rows = queryRows(t, server.URL, `range x from 1 to 2 step 1|as hint.materialized=true T|union T|count`)
	if len(rows) != 1 || rows[0]["Count"] != float64(4) {
		t.Fatalf("as cache=%#v", rows)
	}
	for _, query := range []string{`materialize()`, `materialize(1)`, `print materialize(print x=1)`, `let f=(x:long){toscalar(materialize(print n=x))};datatable(x:long)[1]|project f(x)`} {
		response := queryAPI(t, server.URL, query)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "position") {
			t.Fatalf("%s status=%d body=%s", query, response.StatusCode, body)
		}
	}
}
