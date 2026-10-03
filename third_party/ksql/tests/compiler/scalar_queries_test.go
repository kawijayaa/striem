package compiler_test

import (
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/sqlast"
	"github.com/mattn/go-sqlite3"
)

func TestToScalarValuesAndTypes(t *testing.T) {
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode), ksql.WithCatalog(investigationCatalog()))
		for _, tc := range []struct {
			query, want string
			kind        ksql.ScalarType
		}{
			{`print result=toscalar(2+3)`, `[[5]]`, ksql.TypeLong},
			{`print result=toscalar(print x=7,y="ignored")`, `[[7]]`, ksql.TypeLong},
			{`print result=toscalar(datatable(x:long,y:string)[2,"a",1,"b"] | order by x asc)`, `[[1]]`, ksql.TypeLong},
			{`print result=toscalar((datatable(x:long)[4,8] | take 1))`, `[[4]]`, ksql.TypeLong},
			{`print result=toscalar(datatable(x:long)[])`, `[[<nil>]]`, ksql.TypeLong},
			{`print result=toscalar(print "text")`, `[[text]]`, ksql.TypeString},
			{`print result=toscalar(print false)`, `[[0]]`, ksql.TypeBool},
			{`print result=toscalar(100.0)/3`, `[[33.333333333333336]]`, ksql.TypeReal},
			{`print result=toscalar(2.5)`, `[[2.5]]`, ksql.TypeReal},
			{`print result=toscalar(Events | count)`, `[[2]]`, ksql.TypeLong},
			{`let T=Events | count; print result=toscalar(T)`, `[[2]]`, ksql.TypeLong},
			{`let f=(T:(x:long)){T | summarize total=sum(x)}; print result=toscalar(f(datatable(x:long)[2,5]))`, `[[7]]`, ksql.TypeLong},
			{`let f=(T:(*)){toscalar(T)}; print result=f(datatable(n:long)[5])`, `[[5]]`, ksql.TypeLong},
			{`let f=(x:long){toscalar(x+1)}; print result=f(4)`, `[[5]]`, ksql.TypeLong},
			{`let f=(x:long){x+1}; print result=toscalar(datatable(x:long)[2] | project f(x))`, `[[3]]`, ksql.TypeLong},
			{`let offset=1+2; let total=toscalar(3+offset); print result=total+offset+4`, `[[13]]`, ksql.TypeLong},
			{`let x=toscalar(3); let y=toscalar(x+2); print result=y+x`, `[[8]]`, ksql.TypeLong},
			{`print result=toscalar(print bag=dynamic({"x":7})).x`, `[[7]]`, ksql.TypeDynamic},
		} {
			result := compiler.Compile(tc.query)
			if !result.OK() || len(result.Columns) != 1 || result.Columns[0].Type != tc.kind {
				t.Fatalf("mode=%d %s: %#v", mode, tc.query, result)
			}
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.want {
				t.Fatalf("mode=%d %s: got=%s want=%s SQL=%s Args=%v", mode, tc.query, got, tc.want, result.SQL, result.Args)
			}
		}
		query := `let Start=toscalar(print x=1); let End=toscalar(range x from 1 to 9 step 1 | count); let Step=toscalar(2); range z from Start to End step Step | extend start=Start,end=End,step=Step`
		result := compiler.Compile(query)
		if got := fmt.Sprint(queryRows(t, db, result)); got != `[[1 1 9 2] [3 1 9 2] [5 1 9 2] [7 1 9 2] [9 1 9 2]]` {
			t.Fatal(got)
		}
	}
}

func TestToScalarRejectsCallerRowsAndInvalidForms(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()))
	for _, query := range []string{
		`print toscalar()`, `print toscalar(1,2)`, `print toscalar(x=1)`,
		`print toscalar(Events |)`, `print toscalar(Events | count`,
		`datatable(x:long)[1] | project toscalar(x)`,
		`let f=(x:long){toscalar(x)}; datatable(x:long)[1] | project f(x)`,
		`let T=datatable(x:long,y:long)[1,2]; let f=(x_:long){toscalar(T | where x==x_ | project y)}; datatable(x:long)[1] | project f(x)`,
		`let f=(x:long){let T=datatable(n:long)[1] | extend n=x; toscalar(T)}; datatable(x:long)[1] | project f(x)`,
		`let f=(x:long){let T=datatable(n:long)[1] | extend n=x; let U=T; toscalar(U | project n)}; datatable(x:long)[1] | project f(x)`,
		`let f=(x:long){let hidden=(){x}; toscalar(hidden())}; datatable(x:long)[1] | project f(x)`,
		`let f=(x:long){let unused=datatable(n:long)[1] | where n==x | as T; toscalar(T | count)}; datatable(x:long)[1] | project f(x)`,
		`let f=(x:dynamic){toscalar(x.a)}; datatable(x:dynamic)[dynamic({"a":1})] | project f(x)`,
	} {
		result := compiler.Compile(query)
		if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Span.Line < 1 {
			t.Fatalf("accepted %s: %#v", query, result)
		}
	}
	result := ksql.New(dialect.SQLite()).Compile(`print toscalar(Events | take 1)`)
	if result.OK() || !strings.Contains(fmt.Sprint(result.Diagnostics), "first output column") {
		t.Fatalf("unknown first column: %#v", result)
	}
	for _, target := range []dialect.Dialect{dialect.ANSI(), dialect.MySQL(), dialect.SQLServer()} {
		result := ksql.New(target).Compile(`print toscalar(1)`)
		if result.OK() || result.Diagnostics[0].Span.Line < 1 || !strings.Contains(fmt.Sprint(result.Diagnostics), "materialized") {
			t.Fatalf("dialect=%s: %#v", target.Name(), result)
		}
	}
}

func TestToScalarEvaluatesExactlyOnceAndPrunesUnusedBindings(t *testing.T) {
	var count atomic.Int64
	driver := "toscalar_" + t.Name()
	sql.Register(driver, &sqlite3.SQLiteDriver{ConnectHook: func(connection *sqlite3.SQLiteConn) error {
		return connection.RegisterFunc("test_tick", func() int64 { return count.Add(1) }, false)
	}})
	db, err := sql.Open(driver, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tick := ksql.WithTypedFunction("tick", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 0 {
			return ksql.TypedExpression{}, fmt.Errorf("tick takes no arguments")
		}
		return ksql.TypedExpression{SQL: &sqlast.Call{Name: "test_tick"}, Type: ksql.TypeLong}, nil
	})
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), tick, ksql.WithParameterization(mode))
		for _, tc := range []struct {
			query, want string
			calls       int64
		}{
			{`let x=toscalar(tick()); datatable(n:long)[1,2,3] | project a=x,b=x,c=x+1`, `[[1 1 2] [1 1 2] [1 1 2]]`, 1},
			{`let unused=toscalar(tick()); print result=7`, `[[7]]`, 0},
			{`let f=(x:long){toscalar(x+tick())}; print result=f(2)`, `[[3]]`, 1},
			{`let f=(){toscalar(tick())}; print result=7`, `[[7]]`, 0},
			{`let x=toscalar(tick()); let y=toscalar(x+tick()); datatable(n:long)[1,2] | project a=x,b=y`, `[[1 3] [1 3]]`, 2},
			{`let x=toscalar(datatable(n:long)[1,2,3] | project tick()); datatable(n:long)[1,2] | project a=x,b=x`, `[[1 1] [1 1]]`, 1},
			{`let x=tick(); datatable(n:long)[1,2,3] | project a=x`, `[[1] [2] [3]]`, 3},
		} {
			count.Store(0)
			result := compiler.Compile(tc.query)
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.want || count.Load() != tc.calls {
				t.Fatalf("mode=%d %s: got=%s calls=%d want=%s calls=%d SQL=%s", mode, tc.query, got, count.Load(), tc.want, tc.calls, result.SQL)
			}
			// A materialized scalar is recomputed for each execution of the query.
			count.Store(0)
			queryRows(t, db, result)
			if count.Load() != tc.calls {
				t.Fatal("execution did not recompute scalar")
			}
		}
	}
}

func TestToScalarDoesNotShadowPhysicalTableNames(t *testing.T) {
	db := openInvestigationDB(t)
	if _, err := db.Exec(`CREATE TABLE "__ksql_scalar_0" (x INTEGER); INSERT INTO "__ksql_scalar_0" VALUES (7)`); err != nil {
		t.Fatal(err)
	}
	catalog := testCatalog{"T": {Name: "__ksql_scalar_0", Schema: ksql.Schema{Columns: []ksql.Column{{Name: "x", Type: ksql.TypeLong}}}}}
	result := ksql.New(dialect.SQLite(), ksql.WithCatalog(catalog)).Compile(`let x=toscalar(T | project x); T | project result=x+toscalar(1)`)
	if got := fmt.Sprint(queryRows(t, db, result)); got != `[[8]]` {
		t.Fatal(got)
	}
	if strings.Contains(result.SQL, `"__ksql_scalar_0" AS MATERIALIZED`) {
		t.Fatal("CTE shadows physical table")
	}
	result = ksql.New(dialect.PostgreSQL(), ksql.WithParameters()).Compile(`let unused=toscalar(9); let x=toscalar(4); print y=x+2`)
	if !result.OK() || strings.Contains(result.SQL, "$3") || fmt.Sprint(result.Args) != `[4 2]` || !strings.Contains(result.SQL, "AS MATERIALIZED") {
		t.Fatalf("Postgres SQL/args: %#v", result)
	}
}

func TestToScalarBindingLimits(t *testing.T) {
	var query strings.Builder
	for i := 0; i < 1025; i++ {
		fmt.Fprintf(&query, "let v%d=toscalar(%d);", i, i)
	}
	query.WriteString("print 1")
	result := ksql.New(dialect.SQLite()).Compile(query.String())
	if result.OK() || result.SQL != "" || !strings.Contains(fmt.Sprint(result.Diagnostics), "binding work limit") {
		t.Fatalf("unbounded bindings: %#v", result.Diagnostics)
	}
}
