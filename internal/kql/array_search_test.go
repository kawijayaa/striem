package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestArrayIndexOfExamples(t *testing.T) {
	db := featureDatabase(t)
	query := `let arr=dynamic(["this","is","an","example","an","example"]); print idx1=array_index_of(arr,"an"),idx2=array_index_of(arr,"example",1,3),idx3=array_index_of(arr,"example",1,2),idx4=array_index_of(arr,"is",2,4),idx5=array_index_of(arr,"example",2,-1),idx6=array_index_of(arr,"an",1,-1,2),idx7=array_index_of(arr,"an",1,-1,3),idx8=array_index_of(arr,"an",-3),idx9=array_index_of(arr,"is",-4)`
	if got := fmt.Sprint(featureRows(t, db, query)); got != `[[2 3 -1 -1 3 4 -1 4 -1]]` {
		t.Fatal(got)
	}
	for _, tc := range []struct{ query, want string }{
		{`print a=array_index_of(dynamic([true,1,false,0]),1),b=array_index_of(dynamic([true,1,false,0]),true),c=array_index_of(dynamic([true,1,false,0]),0),d=array_index_of(dynamic([true,1,false,0]),false)`, `[[1 0 3 2]]`},
		{`print a=array_index_of(dynamic([9007199254740992,9007199254740993]),9007199254740993)`, `[[1]]`},
		{`print a=array_index_of(dynamic([1.0,1e3,"1"]),1),b=array_index_of(dynamic([1.0,1e3,"1"]),1000),c=array_index_of(dynamic([1.0,1e3,"1"]),"1")`, `[[0 1 2]]`},
		{`print a=array_index_of(dynamic(["é\u0000","É"]),"é\u0000"),b=array_index_of(dynamic(["É"]),"é")`, `[[0 -1]]`},
		{`print a=array_index_of(dynamic([{},[1],null,2]),2)`, `[[3]]`},
		{`print a=array_index_of(dynamic([1,2,1]),1,0,1,2)`, `[[-1]]`},
		{`print a=array_index_of(dynamic([1,2]),1,0,0),b=array_index_of(dynamic([1,2]),1,9223372036854775807)`, `[[-1 -1]]`},
		{`print a=array_index_of(dynamic([1,2]),2,0,9223372036854775807)`, `[[1]]`},
		{`let start=1; let occurrence=2; print a=array_index_of(dynamic([1,1,1]),1,start,-1,occurrence)`, `[[2]]`},
		{`Events | where n == 1 | project a=array_index_of(items,n)`, `[[0]]`},
		{`print a=array_index_of(dynamic([1]),1,0,-2),b=array_index_of(dynamic([1]),1,0,-1,-1)`, `[[<nil> <nil>]]`},
		{`print a=set_has_element(dynamic([true]),1),b=set_has_element(dynamic([1]),true),c=set_has_element(dynamic([true]),true),d=set_has_element(dynamic([false]),false)`, `[[0 0 1 1]]`},
		{`print a=set_has_element(dynamic([false]),true),b=set_has_element(dynamic([true]),false)`, `[[0 0]]`},
		{`print a=set_has_element(dynamic([true]),true) | where a | count`, `[[1]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	if got := fmt.Sprint(featureRows(t, db, `print index=array_index_of(dynamic([1]),1),member=set_has_element(dynamic([1]),1) | getschema | project ColumnName,ColumnType`)); got != `[[index long] [member bool]]` {
		t.Fatal(got)
	}
	for _, expr := range []string{`array_index_of()`, `array_index_of(dynamic([]))`, `array_index_of("[]",1)`, `array_index_of(dynamic([]),1,0.5)`, `array_index_of(dynamic([]),1,0,-1,"2")`, `array_index_of(items,n,0,-1,n)`, `set_has_element("[]",1)`, `set_has_element(dynamic([]),1,0)`} {
		if _, err := Compile(`Events | project a=`+expr, time.Now(), featureCatalog()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}
