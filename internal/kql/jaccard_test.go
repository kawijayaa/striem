package kql

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestJaccardIndex(t *testing.T) {
	db := featureDatabase(t)
	query := `print set1=dynamic([1,2,3]),set2=dynamic([1,2,3,4]) | extend jaccard=jaccard_index(set1,set2)`
	if got := fmt.Sprint(featureRows(t, db, query)); got != `[[[1,2,3] [1,2,3,4] 0.75]]` {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		left, right string
		want        float64
	}{
		{`dynamic([1,1,2])`, `dynamic([2,3,3])`, 1.0 / 3},
		{`dynamic([1,2])`, `dynamic([2,1,2])`, 1},
		{`dynamic([1,2])`, `dynamic([3,4])`, 0},
		{`dynamic([])`, `dynamic([1])`, 0},
		{`dynamic(["a","b"])`, `dynamic(["b","c"])`, 1.0 / 3},
		// Typed/nested equality is characterized pending differential conformance.
		{`dynamic([true,1])`, `dynamic([1])`, 0.5},
		{`dynamic([9007199254740993])`, `dynamic([9007199254740992])`, 0},
		{`dynamic([{"a":1,"b":2},[1,2]])`, `dynamic([[1,2],{"b":2,"a":1}])`, 1},
		{`dynamic([null,"null"])`, `dynamic([null])`, 0.5},
	} {
		for _, args := range []string{tc.left + "," + tc.right, tc.right + "," + tc.left} {
			rows := featureRows(t, db, `print j=jaccard_index(`+args+`)`)
			got, ok := rows[0][0].(float64)
			if !ok || math.Abs(got-tc.want) > 1e-15 {
				t.Fatalf("%s: got %v want %v", args, rows, tc.want)
			}
		}
	}
	query = `let input=dynamic([1,2]); Events | where n == 1 | project j=jaccard_index(items,input) | where j == 1 | count`
	if got := fmt.Sprint(featureRows(t, db, query)); got != `[[1]]` {
		t.Fatal(got)
	}
	query = `print jaccard_index(dynamic([1]),dynamic([2])) | getschema | project ColumnName,ColumnType`
	if got := fmt.Sprint(featureRows(t, db, query)); got != `[[print_0 real]]` {
		t.Fatal(got)
	}
	for _, expr := range []string{`jaccard_index()`, `jaccard_index(dynamic([]))`, `jaccard_index("[]",dynamic([]))`, `jaccard_index(dynamic([]),1)`, `jaccard_index(dynamic([]),dynamic([]),dynamic([]))`} {
		if _, err := Compile(`print j=`+expr, time.Now()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}
