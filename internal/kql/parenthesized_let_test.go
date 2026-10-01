package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestParenthesizedTabularLets(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`let T=((Events | where n == 1)); T | project user`, `[[alice]]`},
		{`let T=((datatable(x:long)[1,2])); T | summarize n=count()`, `[[2]]`},
		{`let T=((range x from 1 to 3 step 1)); T | count`, `[[3]]`},
		{`let T=((print d=parse_json('{"a":107}'))); T | project d.a`, `[[107]]`},
		{`let T=((print x=1)); T | union ((print x=2)) | order by x asc`, `[[1] [2]]`},
		{`let n=((1)+(2)); print x=n`, `[[3]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	result, err := Compile(`let T=((print d=dynamic([1]),b=not(false))); T | project d,b`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.DynamicColumns["d"]; !ok {
		t.Fatal("dynamic metadata lost")
	}
	if _, ok := result.BooleanColumns["b"]; !ok {
		t.Fatal("Boolean metadata lost")
	}
}
