package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSetExamplesAndComposition(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`range x from 1 to 3 step 1 | extend y=x*2 | extend z=y*2 | extend w=z*2 | extend a1=pack_array(x,y,x,z),a2=pack_array(x,y),a3=pack_array(w) | project Out=set_union(a1,a2,a3)`, `[[[1,2,4,8]] [[2,4,8,16]] [[3,6,12,24]]]`},
		{`datatable(Arr1:dynamic)[dynamic(["A4","A2","A7","A2"]),dynamic(["C4","C7","C1","C4"])] | project Out=set_union(Arr1,Arr1)`, `[[["A4","A2","A7"]] [["C4","C7","C1"]]]`},
		{`range x from 1 to 3 step 1 | extend y=x*2 | extend z=y*2 | extend w=z*2 | extend a1=pack_array(x,y,x,z),a2=pack_array(x,y),a3=pack_array(w,x) | project set_intersect(a1,a2,a3)`, `[[[1]] [[2]] [[3]]]`},
		{`print arr=set_intersect(dynamic([1,2,3]),dynamic([4,5]))`, `[[[]]]`},
		{`range x from 1 to 3 step 1 | extend y=x*2 | extend z=y*2 | extend w=z*2 | extend a1=pack_array(x,y,x,z),a2=pack_array(x,y),a3=pack_array(x,y,w) | project set_difference(a1,a2,a3)`, `[[[4]] [[8]] [[12]]]`},
		{`print arr=set_difference(dynamic([1,2,3]),dynamic([1,2,3]))`, `[[[]]]`},
		{`print a=set_union(dynamic([]),dynamic([2,1,2])),b=set_intersect(dynamic([]),dynamic([1])),c=set_difference(dynamic([1,1]),dynamic([]))`, `[[[2,1] [] [1]]]`},
		{`print a=set_intersect(dynamic([3,2,1,2]),dynamic([1,2,2]),dynamic([2,3]))`, `[[[2]]]`},
		{`print a=set_difference(dynamic([1,2,3,4,1]),dynamic([2,2]),dynamic([4]))`, `[[[1,3]]]`},
		{`let a=dynamic([1,2]); print b=set_union(a,pack_array(2,3))[2]`, `[[3]]`},
		{`Events | where n == 1 | project a=set_union(items,pack_array(n)) | mv-expand a | summarize total=sum(tolong(a))`, `[[3]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	for _, name := range []string{"set_union", "set_intersect", "set_difference"} {
		compiled, err := Compile(`print `+name+`(dynamic([]),dynamic([])) | project renamed=Column1`, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := compiled.DynamicColumns["renamed"]; !ok {
			t.Fatal("dynamic metadata lost")
		}
		for _, expr := range []string{name + `()`, name + `(dynamic([]))`, name + `("[]",dynamic([]))`, name + `(dynamic([]),true)`, name + `(` + strings.TrimSuffix(strings.Repeat(`dynamic([]),`, 65), ",") + `)`} {
			if _, err := Compile(`print a=`+expr, time.Now()); err == nil {
				t.Errorf("accepted %s", expr)
			}
		}
		query := `print a=` + name + `(` + strings.TrimSuffix(strings.Repeat(`dynamic([1]),`, 64), ",") + `)`
		want := `[[[1]]]`
		if name == "set_difference" {
			want = `[[[]]]`
		}
		if got := fmt.Sprint(featureRows(t, db, query)); got != want {
			t.Fatalf("got %s want %s", got, want)
		}
	}
}

// Nested/null equality and cross-numeric comparisons are characterized here;
// full tagged dynamic types and differential Kusto evidence are still needed.
func TestSetDynamicValues(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=set_union(dynamic([true,1,false,0]),dynamic([true,1.0,false,0e3]))`, `[[[true,1,false,0]]]`},
		{`print a=set_union(dynamic([9007199254740992,9007199254740993]),dynamic([9007199254740993]))`, `[[[9007199254740992,9007199254740993]]]`},
		{`print a=set_union(dynamic([null,"null"]),dynamic([null,"null"]))`, `[[[null,"null"]]]`},
		{`print a=set_union(dynamic([{"a":1,"b":[true,null]},[1,2],"é\u0000"]),dynamic([{"b":[true,null],"a":1.0},[1.0,2],[2,1],"é\u0000"]))`, `[[[{"a":1,"b":[true,null]},[1,2],"é\u0000",[2,1]]]]`},
		{`print a=set_intersect(dynamic([{"a":1,"b":2},[1,2],false,0]),dynamic([{"b":2,"a":1},[2,1],0]))`, `[[[{"a":1,"b":2},0]]]`},
		{`print a=set_difference(dynamic([true,1]),dynamic([1]))`, `[[[true]]]`},
		{`print a=set_union(dynamic(["A"]),dynamic(["a"]))`, `[[["A","a"]]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}
