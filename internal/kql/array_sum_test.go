package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestArraySum(t *testing.T) {
	db := featureDatabase(t)
	for _, name := range []string{"array_sum", "series_sum"} {
		for _, tc := range []struct{ array, want string }{
			{`dynamic([1,2,3,4])`, `10`},
			{`dynamic([0.5,0.25,-1])`, `-0.25`},
			{`dynamic([1e2,2e-1])`, `100.2`},
			{`dynamic([1,"2"])`, `<nil>`},
			{`dynamic([true,1])`, `<nil>`},
			{`dynamic([1,null])`, `<nil>`},
			{`dynamic([1,[2]])`, `<nil>`},
			{`dynamic([1,{}])`, `<nil>`},
			{`pack_array(0.5,0.25)`, `0.75`},
		} {
			query := `print a=` + name + `(` + tc.array + `)`
			t.Run(query, func(t *testing.T) {
				if got := fmt.Sprint(featureRows(t, db, query)); got != "[["+tc.want+"]]" {
					t.Fatalf("got %s want %s", got, tc.want)
				}
			})
		}
		query := `let input=dynamic([1,2,3]); print ` + name + `(input) | getschema | project ColumnName,ColumnType`
		if got := fmt.Sprint(featureRows(t, db, query)); got != `[[print_0 real]]` {
			t.Fatal(got)
		}
		for _, expr := range []string{name + `()`, name + `("[1]")`, name + `(true)`, name + `(dynamic([1]),dynamic([2]))`} {
			if _, err := Compile(`print a=`+expr, time.Now()); err == nil {
				t.Errorf("accepted %s", expr)
			}
		}
	}
	for _, tc := range []struct{ query, want string }{
		{`Events | project total=array_sum(items) | where total > 0 | summarize total=sum(total)`, `[[6]]`},
		{`print arr=dynamic([1,2,3,4]) | extend arr_sum=array_sum(arr)`, `[[[1,2,3,4] 10]]`},
		{`print arr=dynamic([1,2,3,4]) | extend series_sum=series_sum(arr)`, `[[[1,2,3,4] 10]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}
