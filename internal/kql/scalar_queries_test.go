package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestToScalarExecutionAndDatasetFiltering(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`let total=toscalar(Events | count); Events | summarize n=count() by host | extend percent=100.0*n/total | order by host asc`, `[[a 2 66.66666666666667] [b 1 33.333333333333336]]`},
		{`let total=toscalar(LeftEvents|count); RightEvents|project n=total,user`, `[[2 carol]]`},
		{`let total=toscalar(LeftEvents|count); LeftEvents|where n<total|project user`, `[[alice]]`},
		{`print n=toscalar(print ['a.b']=7,['extra']=8)`, `[[7]]`},
		{`print n=toscalar(datatable(n:long)[])`, `[[<nil>]]`},
		{`print n=toscalar(print "x' OR 1=1 --")`, `[[x' OR 1=1 --]]`},
		{`print n=toscalar(print "a\u0000b")`, "[[a\x00b]]"},
		{`print n=toscalar(print datetime("2026-01-01"))`, `[[2026-01-01T00:00:00.000000000Z]]`},
		{`let Start=toscalar(print x=1); let End=toscalar(range x from 1 to 9 step 1|count); let Step=toscalar(2); range z from Start to End step Step | extend start=Start,end=End,step=Step`, `[[1 1 9 2] [3 1 9 2] [5 1 9 2] [7 1 9 2] [9 1 9 2]]`},
		{`let total=toscalar(LeftEvents|summarize sum(n)); RightEvents|project value=total+n`, `[[6]]`},
		{`let f=(T:(*)){toscalar(T|count)}; print n=f(LeftEvents)`, `[[2]]`},
		{`let f=(x:long){x+1}; print n=toscalar(LeftEvents|project f(n))`, `[[2]]`},
		{`print n=toscalar(print dynamic({"x":2})).x`, `[[2]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`print d=toscalar(print dynamic([1,true,null])),b=toscalar(print not(false))`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["d"]; !ok {
		t.Fatal("lost dynamic scalar metadata")
	}
	if _, ok := compiled.BooleanColumns["b"]; !ok {
		t.Fatal("lost Boolean scalar metadata")
	}
}

func TestToScalarRejectsCallerRows(t *testing.T) {
	for _, query := range []string{
		`LeftEvents | project n=toscalar(n)`,
		`let f=(x:long){toscalar(LeftEvents|where n==x|count)}; RightEvents|project f(n)`,
		`let f=(x:long){let t=LeftEvents|where n==x;toscalar(t|count)}; RightEvents|project f(n)`,
		`let f=(x:long){let hidden=(){x};toscalar(hidden())}; RightEvents|project f(n)`,
	} {
		_, err := Compile(query, time.Now(), featureCatalog())
		if err == nil || !strings.Contains(err.Error(), "line") {
			t.Fatalf("missing positioned error for %s: %v", query, err)
		}
	}
}
