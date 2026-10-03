package compiler_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestTabularQueryFunctions(t *testing.T) {
	db := openInvestigationDB(t)
	if _, err := db.Exec(`CREATE TABLE quoted ("some column" INTEGER); INSERT INTO quoted VALUES (3)`); err != nil {
		t.Fatal(err)
	}
	catalog := investigationCatalog()
	catalog["Quoted"] = ksql.Table{Name: "quoted", Schema: ksql.Schema{Columns: []ksql.Column{{Name: "some column", Type: ksql.TypeLong}}}}
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(catalog), ksql.WithParameterization(mode))
		for _, tc := range []struct{ query, want string }{
			{`let f=(){print 1}; f`, `[[1]]`},
			{`let f=(){range x from 1 to 3 step 1}; f() | where x>1`, `[[2] [3]]`},
			{`let f=(){print x=2}; let T=f(); let U=T; U`, `[[2]]`},
			{`let f=(){print x=2}; let T=f; T`, `[[2]]`},
			{`let f=(v:long=4){print x=v}; f()`, `[[4]]`},
			{`let f=(T:(x:long),v:long){T|where x>=v}; f((range x from 1 to 10 step 1),9)`, `[[9] [10]]`},
			{`let f=(T:(*)){T|distinct *}; f((datatable(x:long)[1,1,2])) | order by x asc`, `[[1] [2]]`},
			{`let f=(T:(*)){T|count}; f(datatable(x:long)[1,2])`, `[[2]]`},
			{`let f=(T:(*)){T|project *}; f(datatable(x:long)[1,2])`, `[[1] [2]]`},
			{`let f=(T:(*)){union T,T}; f(datatable(x:long)[1])`, `[[1] [1]]`},
			{`let f=(T:(*)){T|where false}; f(datatable(x:long)[1]) | count`, `[[0]]`},
			{`let f=(T:(x:long,y:string)){T}; f(datatable(y:string,x:long)["a",2])`, `[[a 2]]`},
			{`let f=(T: (['some column']:long)){T|project ['some column']}; f(Quoted)`, `[[3]]`},
			{`let f=(){let T=(print x=7); T}; f()`, `[[7]]`},
			{`let f=(T:(x:long)){T}; f(datatable(extra:string,x:long)["a",2])`, `[[a 2]]`},
			{`let f=(T:(x:long),v:long=2){T|where x>=v}; datatable(x:long)[1,2,3] | invoke f()`, `[[2] [3]]`},
			{`let f=(T:(a:string),s:string){T|extend a=strcat(a," ",s)}; datatable(a:string)["one","two"] | invoke f("done")`, `[[one done] [two done]]`},
			{`let f=(T:(x:long),v:long){T|where x>v}; f(v=1,T=(datatable(x:long)[1,2]))`, `[[2]]`},
			{`let f=(T:(x:long),v:long){T|where x>v}; datatable(x:long)[1,2] | invoke f(v=1)`, `[[2]]`},
			{`let f=(A:(x:long),B:(x:long)){union A,B}; f((print x=1),(print x=2))`, `[[1] [2]]`},
			{`let base=(print x=7); let f=(){base}; let g=(){let base=(print x=9); f()}; g()`, `[[7]]`},
			{`let f=(T:(x:long)){let g=(){T|where x>1}; g()}; f((print x=2))`, `[[2]]`},
			{`let f=(T:(x:long)){let g=(U:(x:long)){U}; g(T)}; f((print x=3))`, `[[3]]`},
			{`let f=(){Events}; f | project Host | order by Host asc`, `[[alpha] [beta]]`},
			{`let f=(){print 3 | extend 4}; union (f()),(f()) | project Column1`, `[[4] [4]]`},
			{`let f=(T:(x:long)){1}; print result=f((print x=2))`, `[[1]]`},
			{`let f=(){3}; range x from 1 to 2 step 1 | project y=x*f,z=x*f()`, `[[3 3] [6 6]]`},
		} {
			result := compiler.Compile(tc.query)
			if !result.OK() {
				t.Fatalf("%s: %v", tc.query, result.Diagnostics)
			}
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.want {
				t.Fatalf("%s: got=%s want=%s SQL=%s", tc.query, got, tc.want, result.SQL)
			}
		}
	}
}

func TestTabularQueryFunctionDiagnostics(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()))
	for _, query := range []string{
		`let f=(T:(x:long)){T}; f((print y=1))`,
		`let f=(T:(x:long)){T}; f((print x="a"))`,
		`let f=(T:(x:long)){T}; f(2)`,
		`let f=(T:(x:long)){T}; f()`,
		`let f=(T:(*)){T|project x}; print 1`,
		`let f=(T:(x:long)){T|project extra}; print 1`,
		`let f=(x:long,T:(*)){T}; print 1`,
		`let f=(T:(*)=1){T}; print 1`,
		`let f=(T:(x:long,x:long)){T}; print 1`,
		`let f=(T:(x:decimal)){T}; print 1`,
		`let f=(T:()){T}; print 1`,
		`let f=(){print 1}; print f()`,
		`let f=(){1}; f()`,
		`let f=(x:long){print x}; print 1 | invoke f(2)`,
		`let f=(T:(*)){1}; print 1 | invoke f()`,
		`let f=(T:(*),x:long){T}; print 1 | invoke f()`,
		`let f=(T:(*)){T}; print 1 | invoke f(T=(print 2))`,
		`let f=(T:(*)){union kind=inner T,(print y=1)}; f((print x=1))`,
		`let f=(T:(x:long),v:long){T}; print x=1 | invoke f(x)`,
		`let f=(T:(*)){T}; print 1 | invoke f`,
		`print 1 | invoke missing()`, `print 1 | invoke`,
		`let f=(){let Hidden=(print x=1); Hidden}; Hidden`,
	} {
		result := compiler.Compile(query)
		if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Span.Line < 1 {
			t.Fatalf("accepted invalid function %s: %#v", query, result)
		}
	}
}

func TestTabularFunctionExpansionLimit(t *testing.T) {
	for _, schema := range []string{"(x:long)", "(*)"} {
		query := `let f=(T:` + schema + `){union T,T}; f(` + strings.Repeat("f(", 25) + `(print x=1)` + strings.Repeat(")", 26)
		result := ksql.New(dialect.SQLite()).Compile(query)
		if result.OK() || result.SQL != "" || !strings.Contains(fmt.Sprint(result.Diagnostics), "work limit") {
			t.Fatalf("unbounded expansion: %v", result.Diagnostics)
		}
	}
}
