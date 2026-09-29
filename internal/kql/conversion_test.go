package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestNumericConversionsExecute(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ expr, want string }{
		{`toint("bad"),tolong("12tail"),toreal(""),todouble("3.2tail")`, `[[<nil> <nil> <nil> <nil>]]`},
		{`toint(2.9),toint(-2.9),tolong(3.9),tolong(-3.9)`, `[[2 -2 3 -3]]`},
		{`toint("2147483647"),toint("2147483648"),toint("-2147483648"),toint("-2147483649")`, `[[2147483647 <nil> -2147483648 <nil>]]`},
		{`tolong("9223372036854775807"),tolong("9223372036854775808"),tolong("-9223372036854775808"),tolong("-9223372036854775809")`, `[[9223372036854775807 <nil> -9223372036854775808 <nil>]]`},
		{`tolong("9007199254740993"),tolong("9007199254740993.75"),tolong("9.007199254740993e15")`, `[[9007199254740993 9007199254740993 9007199254740993]]`},
		{`tolong(9223372036854775808.0),tolong(-9223372036854775808.0),toint(2147483647.9),toint(-2147483648.9)`, `[[<nil> -9223372036854775808 2147483647 -2147483648]]`},
		{`toint(null),tolong(null),toreal(null),tobool(null)`, `[[<nil> <nil> <nil> <nil>]]`},
		{`tobool("true"),tobool("false"),toboolean(123),tobool(0),tobool("invalid")`, `[[1 0 1 0 <nil>]]`},
		{`toreal("2.5"),todouble("-2.5e2"),toreal("1e999999999"),tolong("1e-999999999")`, `[[2.5 -250 <nil> 0]]`},
		{`toint("0x10"),tolong("1_000"),toreal("1/2"),toint(" ")`, `[[<nil> <nil> <nil> <nil>]]`},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, `Events | take 1 | project `+tc.expr)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	union := `Events | take 1 | project Value=toint("1") | union (Events | take 1 | project Value=tolong("2"))`
	if got := fmt.Sprint(featureRows(t, db, union)); got != "[[1 <nil>] [<nil> 2]]" {
		t.Fatal(got)
	}
	for _, expr := range []string{`toint()`, `tolong(1,2)`, `toreal()`, `todouble(1,2)`, `tobool()`, `toboolean(true,false)`} {
		if _, err := Compile(`Events | project `+expr, time.Now()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
	compiled, err := Compile(`Events | project B=tobool("false") | project Renamed=B`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.BooleanColumns["Renamed"]; !ok {
		t.Fatal("Boolean conversion metadata lost")
	}
}
