package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestArgMinMaxExecutionAndDatasetFiltering(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`Events|summarize arg_max(n,user) by host|order by host asc`, `[[a 3 carol] [b 2 bob]]`},
		{`Events|summarize latest=arg_max(n,user),earliest=arg_min(n,user),total=count() by host|order by host asc`, `[[a 3 carol 1 alice 2] [b 2 bob 2 bob 1]]`},
		{`LeftEvents|summarize arg_max(n,user),arg_min(n,user)`, `[[2 bob 1 alice]]`},
		{`LeftEvents|summarize (last,lastUser)=arg_max(n,user),(first,firstUser)=arg_min(n,user)`, `[[2 bob 1 alice]]`},
		{`let T=materialize(LeftEvents);T|summarize arg_max(n,*)|project user,n`, `[[bob 2]]`},
		{`print n=1,s="a\u0000b",d=datetime("2026-01-01"),b=true|summarize arg_max(d,*)|project d,n,s,b`, "[[2026-01-01T00:00:00.000000000Z 1 a\x00b 1]]"},
		{`datatable(n:long,s:string)[]|summarize arg_max(n,s),total=count()`, `[[<nil> <nil> 0]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`print n=1,d=dynamic([1,true,null]),b=true|summarize arg_max(n,*)`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["d"]; !ok {
		t.Fatal("lost dynamic output metadata")
	}
	if _, ok := compiled.BooleanColumns["b"]; !ok {
		t.Fatal("lost Boolean output metadata")
	}
	for _, query := range []string{`Events|summarize arg_max(user+"x",*)`, `print true+1`, `Events|summarize (a,b)=arg_max(n)`, `Events|project arg_min(n,user)`} {
		_, err := Compile(query, time.Now(), featureCatalog())
		if err == nil || !strings.Contains(err.Error(), "line") {
			t.Fatalf("missing positioned error %s: %v", query, err)
		}
	}
}
