package ksql_test

import (
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestIntTypeAndNumericPromotion(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []ksql.Column
	}{
		{`print a=int(1),b=toint("2"),c=long(3)`, []ksql.Column{{Name: "a", Type: ksql.TypeInt}, {Name: "b", Type: ksql.TypeInt}, {Name: "c", Type: ksql.TypeLong}}},
		{`datatable(a:int)[1] | project b=a+long(1),c=long(1)+a,d=a+0.5`, []ksql.Column{{Name: "b", Type: ksql.TypeLong}, {Name: "c", Type: ksql.TypeLong}, {Name: "d", Type: ksql.TypeReal}}},
		{`datatable(a:int)[1] | summarize S=sum(a),C=sumif(a,true),M=min(a)`, []ksql.Column{{Name: "S", Type: ksql.TypeLong}, {Name: "C", Type: ksql.TypeLong}, {Name: "M", Type: ksql.TypeInt}}},
		{`print x=int(1) | union (print x=long(2))`, []ksql.Column{{Name: "x_int", Type: ksql.TypeInt}, {Name: "x_long", Type: ksql.TypeLong}}},
	} {
		result := ksql.New(dialect.SQLite(), ksql.WithFunction("toint", ksql.SQLFunction("custom_toint"))).Compile(tc.query)
		if !result.OK() || len(result.Columns) != len(tc.want) {
			t.Fatalf("%s: %#v", tc.query, result)
		}
		for i, c := range tc.want {
			if result.Columns[i] != c {
				t.Errorf("%s: columns=%v want=%v", tc.query, result.Columns, tc.want)
				break
			}
		}
	}
}
