package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTakeAnyDatasetSelectionAndTypes(t *testing.T) {
	db := featureDatabase(t)
	rows := featureRows(t, db, `LeftEvents|summarize take_any(n,user),total=count()`)
	if len(rows) != 1 || rows[0][2] != int64(2) || fmt.Sprint(rows[0][:2]) != `[1 alice]` && fmt.Sprint(rows[0][:2]) != `[2 bob]` {
		t.Fatalf("rows=%#v", rows)
	}
	for _, tc := range []struct{ query, want string }{
		{`Events|summarize v=take_anyif(user,n==3),total=count()`, `[[carol 3]]`},
		{`LeftEvents|summarize v=take_anyif(user,n==3),total=count()`, `[[<nil> 2]]`},
		{`let T=materialize(LeftEvents);T|summarize take_any(n,user)|join kind=inner (T) on n,user|project n,user|count`, `[[1]]`},
		{`print n=1,s="a\u0000b",d=datetime("2026-01-01"),b=true|summarize take_any(d,n,s,b)`, "[[2026-01-01T00:00:00.000000000Z 1 a\x00b 1]]"},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`print d=dynamic([1,true,null]),b=true|summarize take_any(*)`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["d"]; !ok {
		t.Fatal("lost dynamic metadata")
	}
	if _, ok := compiled.BooleanColumns["b"]; !ok {
		t.Fatal("lost Boolean metadata")
	}
	for _, q := range []string{`Events|project take_any(n)`, `Events|summarize take_anyif(user,1)`, `Events|summarize (a,b)=take_any(n)`} {
		_, err := Compile(q, time.Now(), featureCatalog())
		if err == nil || !strings.Contains(err.Error(), "line") {
			t.Fatalf("missing positioned error %s: %v", q, err)
		}
	}
}
