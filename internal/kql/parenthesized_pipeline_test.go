package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestParenthesizedPipelineSources(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`(Events | where n>1) | project user | order by user asc`, `[[bob] [carol]]`},
		{`((LeftEvents | project user,n) | where n==2) | project user`, `[[bob]]`},
		{`(((RightEvents))) | project user`, `[[carol]]`},
		{`((LeftEvents | project user) | union (RightEvents | project user)) | order by user asc`, `[[alice] [bob] [carol]]`},
		{`((LeftEvents | project n) | join kind=inner (datatable(n:long,x:string)[2,"matched"]) on n) | project x`, `[[matched]]`},
		{`let T=((LeftEvents | project n,user) | where n==1); (T) | project user`, `[[alice]]`},
		{`(print d=parse_json('{"a":[157]}'),b=not(false)) | project d.a[0],b`, `[[157 1]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	result, err := Compile(`(print d=dynamic([1]),b=not(false)) | project d,b`, time.Now(), featureCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.DynamicColumns["d"]; !ok {
		t.Fatal("dynamic metadata lost")
	}
	if _, ok := result.BooleanColumns["b"]; !ok {
		t.Fatal("Boolean metadata lost")
	}
	for _, query := range []string{`(Events | take 1`, `(Events |) | take 1`, `() | take 1`, `(1+2) | project x`} {
		if result, err := Compile(query, time.Now(), featureCatalog()); err == nil || result.SQL != "" {
			t.Fatalf("accepted invalid source %s: %#v, %v", query, result, err)
		}
	}
}
