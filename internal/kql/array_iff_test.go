package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestArrayIff(t *testing.T) {
	db := featureDatabase(t)
	for _, name := range []string{"array_iff", "array_iif"} {
		for _, tc := range []struct{ args, want string }{
			{`dynamic([true,false,true]),dynamic([1,2,3]),dynamic([4,5,6])`, `[1,5,3]`},
			{`dynamic([1,0,50]),"yes","no"`, `["yes","no","yes"]`},
			{`dynamic([true,true,true]),dynamic([1,2]),dynamic([3,4])`, `[1,2,null]`},
			{`dynamic(["text",null,{},[1]]),1,0`, `[null,null,null,null]`},
			{`dynamic([0,-1,0.5,-0.0]),true,false`, `[false,true,true,false]`},
			{`dynamic([true,false,true]),dynamic([]),dynamic([1])`, `[null,null,null]`},
			{`dynamic([true,false]),"[1,2]","{\"a\":1}"`, `["[1,2]","{\"a\":1}"]`},
			{`dynamic([true,false]),bag_pack("long",9223372036854775807),null`, `[{"long":9223372036854775807},null]`},
			{`dynamic([true,false]),dynamic([[1,2]]),dynamic([null,{"x":true}])`, `[[1,2],{"x":true}]`},
			{`dynamic([true,false]),9223372036854775807,"é\u0000"`, `[9223372036854775807,"é\u0000"]`},
			{`dynamic([]),1,0`, `[]`},
		} {
			query := `print a=` + name + `(` + tc.args + `)`
			t.Run(query, func(t *testing.T) {
				if got := fmt.Sprint(featureRows(t, db, query)); got != "[["+tc.want+"]]" {
					t.Fatalf("got %s want %s", got, tc.want)
				}
			})
		}
		for _, expr := range []string{name + `()`, name + `(dynamic([]),1)`, name + `("[]",1,0)`, name + `(true,1,0)`, name + `(dynamic([]),1,0,2)`} {
			if _, err := Compile(`print a=`+expr, time.Now()); err == nil {
				t.Errorf("accepted %s", expr)
			}
		}
	}
	query := `let yes=dynamic([1,2]); Events | where n == 1 | project a=array_iff(items,yes,0) | mv-expand a | summarize total=sum(tolong(a))`
	if got := fmt.Sprint(featureRows(t, db, query)); got != `[[3]]` {
		t.Fatal(got)
	}
	query = `print a=array_iff(dynamic([true]),1,0)[0]`
	if got := fmt.Sprint(featureRows(t, db, query)); got != `[[1]]` {
		t.Fatal(got)
	}
	compiled, err := Compile(`print array_iff(dynamic([]),1,0) | project renamed=print_0`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["renamed"]; !ok {
		t.Fatal("dynamic metadata lost")
	}
}
