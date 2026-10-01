package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestParseJSONContracts(t *testing.T) {
	db := featureDatabase(t)
	for _, name := range []string{"parse_json", "parsejson", "todynamic", "toobject"} {
		for _, tc := range []struct{ input, want string }{
			{`"not json"`, `"not json"`},
			{`"{bad}"`, `"{bad}"`},
			{`""`, `""`},
			{`"é\u0000"`, `"é\u0000"`},
			{`" { \"long\" : 9223372036854775807 } "`, `{"long":9223372036854775807}`},
			{`" [1, true, null] "`, `[1,true,null]`},
			{`"\"hello\""`, `"hello"`},
			{`"false"`, `false`},
			{`"9223372036854775807"`, `9223372036854775807`},
			{`dynamic([1,true,null])`, `[1,true,null]`},
			{`dynamic("[1,2]")`, `"[1,2]"`},
			{`dynamic("not json")`, `"not json"`},
		} {
			query := `print d=` + name + `(` + tc.input + `)`
			t.Run(query, func(t *testing.T) {
				if got := fmt.Sprint(featureRows(t, db, query)); got != "[["+tc.want+"]]" {
					t.Fatalf("got %s want %s", got, tc.want)
				}
			})
		}
		for _, expr := range []string{name + `()`, name + `(1)`, name + `(true)`, name + `("a","b")`} {
			if _, err := Compile(`print d=`+expr, time.Now()); err == nil {
				t.Errorf("accepted %s", expr)
			}
		}
		compiled, err := Compile(`print d=`+name+`("bad") | project renamed=d`, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := compiled.DynamicColumns["renamed"]; !ok {
			t.Fatal("dynamic metadata lost")
		}
	}
	for _, tc := range []struct{ query, want string }{
		{`Events | where n == 1 | project RawData.user,items[0] | project RawData_user,items_0`, `[[alice 1]]`},
		{`let d=dynamic({"a":{"b":61}}); print d.a.b | project d_a_b`, `[[61]]`},
		{`print d=dynamic({"a.b":67}) | project d.['a.b'] | project ['d_a.b']`, `[[67]]`},

		{`print d=dynamic([11,22,33])[toint("-1")]`, `[[33]]`},
		{`Events | where n == 1 | project d=items[-n],first=items[false],last=items[true]`, `[[2 1 2]]`},
		{`print d=parse_json('[{"x":1},{"x":9007199254740993}]')[(-1)].x`, `[[9007199254740993]]`},
		{`print d=dynamic([11])[-9223372036854775807-1],other=dynamic([11])[9223372036854775807]`, `[[<nil> <nil>]]`},

		{`print d=parse_json('{"where":{"a.b":47}}').['where'].["a.b"]`, `[[47]]`},
		{`Events | where n == 1 | project user=RawData.['user']`, `[[alice]]`},
		{`print d=dynamic({"é":53}).[@'é']`, `[[53]]`},

		{`let d=@'{"a":123,"b":"{\"c\":456}"}'; print d_b_c=parse_json(tostring(parse_json(d).b)).c`, `[[456]]`},
		{`print d=parse_json('{"a":[{"b":23}]}').a[0].b + 1`, `[[24]]`},
		{`print d=bag_pack("a",dynamic({"b":17})).a.b`, `[[17]]`},
		{`print d=parse_json("{bad}").missing`, `[[<nil>]]`},

		{`datatable(context_custom_metrics:string)['{"duration":{"value":118.0,"count":5.0,"min":100.0,"max":150.0,"stdDev":0.0,"sampledValue":118.0,"sum":118.0}}'] | extend d=parse_json(context_custom_metrics) | project duration_value=d.duration.value,duration_min=d.duration.min`, `[[118 100]]`},
		{`let d=@'{"a":123,"b":"{\"c\":456}"}'; print outer=parse_json(d) | extend inner=parse_json(tostring(outer.b)) | project d_b_c=inner.c`, `[[456]]`},
		{`print d=parse_json("not json") | project missing=d.missing`, `[[<nil>]]`},
		{`print d=parse_json("not json") | project d=parse_json(d)`, `[["not json"]]`},
		{`Events | where n == 1 | project d=parse_json(RawData.user)`, `[[alice]]`},
		{`Events | where n == 1 | project d=parse_json(RawData) | project user=d.user`, `[[alice]]`},
		{`let input="not json"; print d=parse_json(input) | where isnotnull(d) | count`, `[[1]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}
