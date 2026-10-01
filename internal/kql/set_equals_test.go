package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestSetEquals(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ left, right, want string }{
		{`[]`, `[]`, `[[1]]`},
		{`[1,2,1]`, `[2,1]`, `[[1]]`},
		{`[1,2]`, `[2,3]`, `[[0]]`},
		{`[]`, `[1]`, `[[0]]`},
		{`[1]`, `[1,2]`, `[[0]]`},
		{`["a","b","a"]`, `["b","a"]`, `[[1]]`},
		{`["A"]`, `["a"]`, `[[0]]`},
		// The following characterize the shared dynamic equality model. Full
		// tagged types and engine-level differential evidence remain required.
		{`[true,1,false,0]`, `[0,false,1,true,1]`, `[[1]]`},
		{`[true]`, `[1]`, `[[0]]`},
		{`[1]`, `["1"]`, `[[0]]`},
		{`[null,"null"]`, `["null",null,null]`, `[[1]]`},
		{`[null]`, `["null"]`, `[[0]]`},
		{`[9007199254740993]`, `[9007199254740992]`, `[[0]]`},
		{`[9223372036854775807]`, `[9223372036854775807]`, `[[1]]`},
		{`[1,0]`, `[1.0,-0e3]`, `[[1]]`},
		{`[{"a":1,"b":[true,null]},[1,2]]`, `[[1.0,2],{"b":[true,null],"a":1.0}]`, `[[1]]`},
		{`[[1,2]]`, `[[2,1]]`, `[[0]]`},
		{`["é\u0000"]`, `["\u00e9\u0000"]`, `[[1]]`},
	} {
		for _, args := range []string{tc.left + `),dynamic(` + tc.right, tc.right + `),dynamic(` + tc.left} {
			query := `print value=set_equals(dynamic(` + args + `))`
			if got := fmt.Sprint(featureRows(t, db, query)); got != tc.want {
				t.Fatalf("%s: got=%s want=%s", query, got, tc.want)
			}
		}
	}
	for _, tc := range []struct{ query, want string }{
		{`let a=dynamic([2,1,2]); Events | where set_equals(items,a) | project user`, `[[alice]]`},
		{`Events | where not(set_equals(items,dynamic([]))) | project user | order by user asc`, `[[alice] [bob]]`},
		{`print set_equals(dynamic([1]),dynamic([1])) | getschema | project ColumnName,ColumnType`, `[[print_0 bool]]`},
		{`print a=set_equals(pack_array(true,false),pack_array(false,true,true)) | extend b=not(a) | project a,b`, `[[1 0]]`},
		{`print a=set_equals(set_union(dynamic([1]),dynamic([2])),dynamic([2,1]))`, `[[1]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	result, err := Compile(`print set_equals(dynamic([1]),dynamic([1])) | project renamed=print_0`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.BooleanColumns["renamed"]; !ok {
		t.Fatal("Boolean metadata lost")
	}
	if _, ok := result.DynamicColumns["renamed"]; ok {
		t.Fatal("Boolean incorrectly marked dynamic")
	}
	for _, expr := range []string{`set_equals()`, `set_equals(dynamic([]))`, `set_equals(dynamic([]),dynamic([]),dynamic([]))`, `set_equals("[]",dynamic([]))`, `set_equals(dynamic([]),1)`, `set_equals(true,dynamic([]))`} {
		if result, err := Compile(`print value=`+expr, time.Now()); err == nil || result.SQL != "" {
			t.Fatalf("accepted %s: %#v, %v", expr, result, err)
		}
	}
}
