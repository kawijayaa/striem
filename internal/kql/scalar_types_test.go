package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestScalarCommonTypesAndLogicalDiagnostics(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print x=coalesce("","second"),y=coalesce("",""),z=coalesce(1,2.0)/2`, `[[second  0.5]]`},
		{`print x=iff(true,1,2.0)/2,y=case(true,1,2.0)/2,z=-(-1)`, `[[0.5 0.5 1]]`},
		{`print x=iff(bool(null),1,2),y=not(bool(null))`, `[[2 <nil>]]`},
		{`print x=iff(true,true,"x"),y=iff(false,1,"x"),z=iff(true,long(null),"x")`, `[[true "x" <nil>]]`},
		{`print x=iff(true,"a\u0000b",1)`, "[[\"a\\u0000b\"]]"},
		{`print x=coalesce(null,datetime("2026-01-01")),y=iff(true,datetime("2026-01-02"),datetime("2026-01-01"))`, `[[2026-01-01T00:00:00.000000000Z 2026-01-02T00:00:00.000000000Z]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`print a=iff(true,true,false),b=case(true,true,false),d=iff(false,1,"x")`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if _, ok := compiled.BooleanColumns[name]; !ok {
			t.Fatalf("missing bool metadata=%s", name)
		}
	}
	if _, ok := compiled.DynamicColumns["d"]; !ok {
		t.Fatal("missing dynamic metadata")
	}
	for _, q := range []string{`print iff(1,2,3)`, `print case(true,1,"bad",2,3)`, `Events|where n and true`, `print -"abc"`, `print coalesce(1,"x")`} {
		if _, err := Compile(q, time.Now(), featureCatalog()); err == nil {
			t.Fatalf("accepted %s", q)
		}
	}
}
