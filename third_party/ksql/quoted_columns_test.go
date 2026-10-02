package ksql_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestQuotedColumnDeclarations(t *testing.T) {
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode))
		for _, tc := range []struct {
			query, want string
			names       []string
		}{
			{`datatable(['some column']:long,['a.b']:string)[3,"x"] | project ['answer name']=['some column']+1,['data dot']=['a.b']`, `[[4 x]]`, []string{"answer name", "data dot"}},
			{`print ['a.b']=dynamic({"x":3}) | extend ['bool value']=not(false) | project ['a.b'].x,['bool value']`, `[[3 1]]`, []string{"a.b_x", "bool value"}},
			{`print ['some value']=2 | extend ['some value']=['some value']+1 | project-rename ['a.b']=['some value'] | project-rename ['renamed value']=['a.b']`, `[[3]]`, []string{"renamed value"}},
			{`datatable(['group key']:string)["a","a","b"] | summarize ['row count']=count() by ['group name']=['group key'] | order by ['group name'] asc`, `[[a 2] [b 1]]`, []string{"group name", "row count"}},
			{`datatable(['é\u002e名']:long)[7] | project ['é.名']`, `[[7]]`, []string{"é.名"}},
			{`datatable([@'a''b']:long)[11] | project ["a'b"]`, `[[11]]`, []string{"a'b"}},
			{`print ['\'quoted\'']=13 | project ['\'quoted\'']`, `[[13]]`, []string{"'quoted'"}},
			{`print ['x"; DROP TABLE events; --']=17 | project ['x"; DROP TABLE events; --']`, `[[17]]`, []string{`x"; DROP TABLE events; --`}},
			{`datatable(['where']:long)[19] | project ['where']`, `[[19]]`, []string{"where"}},
			{`print ['a' ' b']=23 | project ['a b']`, `[[23]]`, []string{"a b"}},
			{`datatable(['a' @' b']:long)[29] | project ['a' ' b']`, `[[29]]`, []string{"a b"}},
			{`let ['some' ' value']=31; print result=['some value']`, `[[31]]`, []string{"result"}},
			{`let f=(['some' ' value']:long){['some value']+1}; print result=f(['some' ' value']=36)`, `[[37]]`, []string{"result"}},
			{`let f=(T:(['some' ' column']:long)){T}; f(datatable(['some column']:long)[41])`, `[[41]]`, []string{"some column"}},
		} {
			result := compiler.Compile(tc.query)
			if !result.OK() {
				t.Fatalf("%s: %v", tc.query, result.Diagnostics)
			}
			var names []string
			for _, column := range result.Columns {
				names = append(names, column.Name)
			}
			if !reflect.DeepEqual(names, tc.names) {
				t.Fatalf("%s: columns=%v want=%v", tc.query, names, tc.names)
			}
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.want {
				t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
			}
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("quoted name affected database: %v %d", err, count)
	}
}

func TestQuotedColumnDeclarationDiagnostics(t *testing.T) {
	compiler := ksql.New(dialect.SQLite())
	for _, query := range []string{
		`datatable(['a']:long,a:long)[1,2]`,
		`datatable([a]:long)[1]`, `datatable(['a'] "string")[1]`,
		`datatable(['a']:"long")[1]`, `datatable(['a']:long,)[1]`,
		`datatable(['a' 1]:long)[1]`, `datatable(['a']:long)[1,2,3] | project ['missing']`,
		`print ['a'] =`, `print ['a.b'].x=1`, `print ['a' 1]=1`,
		`let f=(['a']:long){['a']}; print f(['a']=)`,
		`let f=(['a']:long){['a']}; print f(['a']=`,
		`let f=(['a']:long){['a']}; print f(['b']=1)`,
		`let a=dynamic({"b":42}); print x=1 | project ['a.b']`,
		`print a=dynamic({"b":42}) | project ['a.b']`,
	} {
		result := compiler.Compile(query)
		if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Span.Line < 1 {
			t.Fatalf("accepted invalid declaration %s: %#v", query, result)
		}
	}
}

func TestQuotedDottedColumnWithOpenSchema(t *testing.T) {
	db := openInvestigationDB(t)
	if _, err := db.Exec(`CREATE TABLE quoted ("a.b" INTEGER); INSERT INTO quoted VALUES (43)`); err != nil {
		t.Fatal(err)
	}
	result := ksql.New(dialect.SQLite()).Compile(`quoted | project ['a.b']`)
	if !result.OK() || len(result.Columns) != 1 || result.Columns[0].Name != "a.b" {
		t.Fatalf("quoted column split into path: %#v", result)
	}
	if got := fmt.Sprint(queryRows(t, db, result)); got != "[[43]]" {
		t.Fatal(got)
	}
}
