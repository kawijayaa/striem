package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCoreAggregateTypesAndDefaultsThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `datatable(n:long,b:bool,p:bool)[1,true,true,2,false,true,100,true,false,long(null),true,bool(null)]|summarize s=sumif(n,p),a=avgif(n,p),lo=minif(b,p),hi=maxif(b,p),c=countif(p)`)
	if len(rows) != 1 || rows[0]["s"] != float64(3) || rows[0]["a"] != 1.5 || rows[0]["lo"] != false || rows[0]["hi"] != true || rows[0]["c"] != float64(2) {
		t.Fatalf("typed aggregates=%#v", rows)
	}
	rows = queryRows(t, server.URL, `datatable(n:long,p:bool)[]|summarize s=sum(n),si=sumif(n,p),c=countif(p),a=avgif(n,p),lo=minif(n,p),hi=maxif(n,p)`)
	if len(rows) != 1 || rows[0]["s"] != float64(0) || rows[0]["si"] != float64(0) || rows[0]["c"] != float64(0) || rows[0]["a"] != nil || rows[0]["lo"] != nil || rows[0]["hi"] != nil {
		t.Fatalf("empty aggregates=%#v", rows)
	}
	for _, q := range []string{`print avgif(1,true)`, `print n=1|summarize sum(n) by count()`, `print n=1|summarize avgif(n,1)`, `print n=1|summarize sum(sum(n))`, `print n=1|summarize sum("1")`} {
		response := queryAPI(t, server.URL, q)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "position") {
			t.Fatalf("%s status=%d body=%s", q, response.StatusCode, body)
		}
	}
}
