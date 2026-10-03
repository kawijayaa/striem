package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestMaterializeExecutionAndDatasetFiltering(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`let T=materialize(LeftEvents); union T,T | project user | order by user asc`, `[[alice] [alice] [bob] [bob]]`},
		{`let T=materialize(LeftEvents); let total=toscalar(T|count); RightEvents|extend total=total|project user,total`, `[[carol 2]]`},
		{`LeftEvents | as hint.materialized=true T | join kind=inner (T) on n | project user,user1 | order by user asc`, `[[alice alice] [bob bob]]`},
		{`let T=materialize(print ['a.b']=7,d=datetime("2026-01-01"),s="a\u0000b"); union T,T`, "[[7 2026-01-01T00:00:00.000000000Z a\x00b] [7 2026-01-01T00:00:00.000000000Z a\x00b]]"},
		{`let f=(T:(*)){materialize(T)}; let T=f(LeftEvents); T|count`, `[[2]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`materialize(print d=dynamic([1,true,null]),b=true)`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["d"]; !ok {
		t.Fatal("lost dynamic metadata")
	}
	if _, ok := compiled.BooleanColumns["b"]; !ok {
		t.Fatal("lost Boolean metadata")
	}
}
