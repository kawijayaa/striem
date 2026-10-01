package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestZip(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print zip(dynamic([1,3,5]),dynamic([2,4,6]))`, `[[[[1,2],[3,4],[5,6]]]]`},
		{`print zip(dynamic(["A",1,1.5]),dynamic([{},"B"]))`, `[[[["A",{}],[1,"B"],[1.5,null]]]]`},
		{`datatable(a:int,b:string)[1,"one",2,"two",3,"three"] | summarize a=make_list(a),b=make_list(b) | project zip(a,b)`, `[[[[1,"one"],[2,"two"],[3,"three"]]]]`},
		{`print z=zip(dynamic([]),dynamic([]))`, `[[[]]]`},
		{`print z=zip(dynamic([]),dynamic([1,2]))`, `[[[[null,1],[null,2]]]]`},
		{`print z=zip(dynamic([1,2]),dynamic([]))`, `[[[[1,null],[2,null]]]]`},
		{`print z=zip(dynamic([1]),dynamic([2,3]),dynamic([4,5,6]))`, `[[[[1,2,4],[null,3,5],[null,null,6]]]]`},
		{`print z=zip(dynamic([9223372036854775807,null,true]),dynamic([[1,2],{"x":false},"é\u0000"]))`, `[[[[9223372036854775807,[1,2]],[null,{"x":false}],[true,"é\u0000"]]]]`},
		{`let a=dynamic([1,2]); print z=zip(a,pack_array(true,null)) | project v=z[0][1]`, `[[1]]`},
		{`Events | where n == 1 | project z=zip(items,pack_array(n)) | mv-expand z | project left=tolong(z[0]),right=tolong(z[1])`, `[[1 1] [2 <nil>]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	compiled, err := Compile(`print zip(dynamic([]),dynamic([])) | project renamed=print_0`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["renamed"]; !ok {
		t.Fatal("dynamic metadata lost")
	}
	for _, expr := range []string{`zip()`, `zip(dynamic([]))`, `zip("[]",dynamic([]))`, `zip(dynamic([]),true)`, `zip(` + strings.TrimSuffix(strings.Repeat(`dynamic([]),`, 65), ",") + `)`} {
		if _, err := Compile(`print z=`+expr, time.Now()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
	query := `print z=zip(` + strings.TrimSuffix(strings.Repeat(`dynamic([1]),`, 64), ",") + `) | project cells=array_length(z[0])`
	if got := fmt.Sprint(featureRows(t, db, query)); got != `[[64]]` {
		t.Fatal(got)
	}
}
