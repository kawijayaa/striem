package kql

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestStatisticalAggregates(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`Events | summarize V=variance(n),P=variancep(n),S=stdev(n)`, `[[1 0.6666666666666666 1]]`},
		{`Events | summarize V=varianceif(n,n>1),P=variancepif(n,n>1),S=stdevpif(n,n==2)`, `[[0.5 0.25 0]]`},
		{`Events | summarize V=varianceif(n,false),P=variancepif(n,RawData.missing==1)`, `[[<nil> <nil>]]`},
		{`Events | where n<0 | summarize V=variance(n),P=variancep(n),S=stdev(n),SP=stdevp(n)`, `[[<nil> <nil> <nil> <nil>]]`},
		{`Events | where n==1 | summarize V=variance(n),P=variancep(n)`, `[[<nil> 0]]`},
		{`Events | summarize V=variance(n) by host | sort by host asc`, `[[a 2] [b <nil>]]`},
		{`datatable(x:long)[9007199254740992,9007199254740993,9007199254740994] | summarize V=variance(x),P=variancep(x)`, `[[1 0.6666666666666666]]`},
		{`datatable(x:long)[9223372036854775805,9223372036854775806,9223372036854775807] | summarize V=variance(x)`, `[[1]]`},
		{`datatable(x:real)[1.0,null,2.0,3.0] | summarize V=variance(x)`, `[[1]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	rows := featureRows(t, db, `datatable(x:long)[1,2,3,4,5] | summarize S=stdev(x),P=stdevp(x)`)
	if math.Abs(rows[0][0].(float64)-math.Sqrt(2.5)) > 1e-12 || math.Abs(rows[0][1].(float64)-math.Sqrt(2)) > 1e-12 {
		t.Fatal(rows)
	}
	for _, name := range []string{"variance", "variancep", "stdev", "stdevp"} {
		for _, expr := range []string{name + `()`, name + `(user)`, name + `(n,true)`, name + `if(n,"yes")`} {
			if _, err := Compile(`Events | summarize X=`+expr, time.Now(), featureCatalog()); err == nil {
				t.Errorf("accepted %s", expr)
			}
		}
	}
	conditional := featureRows(t, db, `Events | summarize S=stdevif(n,n>1),P=stdevp(n)`)
	if math.Abs(conditional[0][0].(float64)-math.Sqrt(0.5)) > 1e-12 || math.Abs(conditional[0][1].(float64)-math.Sqrt(2.0/3)) > 1e-12 {
		t.Fatal(conditional)
	}
	compiled, err := Compile(`Events | summarize V=variance(n) | getschema`, time.Now(), featureCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Columns) != 4 {
		t.Fatal(compiled.Columns)
	}
}
