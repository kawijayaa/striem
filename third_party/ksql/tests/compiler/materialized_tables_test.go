package compiler_test

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/sqlast"
	"github.com/mattn/go-sqlite3"
)

func TestMaterializedTableValuesAndSchemas(t *testing.T) {
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode), ksql.WithCatalog(investigationCatalog()))
		for _, tc := range []struct{ query, want string }{
			{`materialize(datatable(x:long,y:string)[2,"b",1,"a"]) | order by x asc`, `[[1 a] [2 b]]`},
			{`let T=materialize(datatable(x:long)[1,2]); union T,T | order by x asc`, `[[1] [1] [2] [2]]`},
			{`let T=materialize(Events | count); T`, `[[2]]`},
			{`let T=materialize(Events); T | count`, `[[2]]`},
			{`let T=materialize(datatable(x:long)[]); T | count`, `[[0]]`},
			{`materialize((print x=3,y="q")) | project y,x`, `[[q 3]]`},
			{`materialize(materialize(print x=4))`, `[[4]]`},
			{`let f=(T:(*)){materialize(T)}; f(datatable(x:long,y:string)[1,"a"])`, `[[1 a]]`},
			{`let f=(x:long){materialize(print n=x+1)}; f(4)`, `[[5]]`},
			{`let f=(T:(x:long)){let U=materialize(T|where x>1); U | union U}; f(datatable(x:long)[1,2])`, `[[2] [2]]`},
			{`let f=(x:long){let g=(){materialize(print n=x)}; g()}; f(7)`, `[[7]]`},
			{`let materialize=(x:long){print n=x}; materialize(7)`, `[[7]]`},
			{`let T=materialize(datatable(x:long)[1,2,3]); let n=toscalar(T|count); T|extend total=n`, `[[1 3] [2 3] [3 3]]`},
			{`let n=toscalar(2); let T=materialize(datatable(x:long)[1,2,3]|where x>n); T`, `[[3]]`},
			{`datatable(x:long)[1,2] | as hint.materialized=true T | union T | order by x asc`, `[[1] [1] [2] [2]]`},
			{`datatable(x:long)[1,2] | as hint.materialized=true T | join kind=inner (T) on x | project x,x1 | order by x asc`, `[[1 1] [2 2]]`},
			{`datatable(x:long)[1,2] | as hint.materialized=false T | union T | count`, `[[4]]`},
			{`let d=materialize(datatable(State:string,EventType:string)["A","X","A","X","A","Y","B","Z"] | summarize Events=count() by State,EventType); d | summarize TotalStateEvents=sum(Events) by State | join (d) on State | extend EventPercentage=Events*100.0/TotalStateEvents | project State,EventType,EventPercentage,Events | order by State asc,EventType asc`, `[[A X 66.66666666666667 2] [A Y 33.333333333333336 1] [B Z 100 1]]`},
		} {
			result := compiler.Compile(tc.query)
			if !result.OK() {
				t.Fatalf("%s: %#v", tc.query, result.Diagnostics)
			}
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.want {
				t.Fatalf("mode=%d %s: got=%s want=%s SQL=%s args=%v", mode, tc.query, got, tc.want, result.SQL, result.Args)
			}
		}
		result := compiler.Compile(`let T=materialize(print ['a.b']=dynamic({"x":1}),flag=true,text="x' OR 1=1 --"); T`)
		want := []ksql.Column{{Name: "a.b", Type: ksql.TypeDynamic}, {Name: "flag", Type: ksql.TypeBool}, {Name: "text", Type: ksql.TypeString}}
		if !result.OK() || !reflect.DeepEqual(result.Columns, want) {
			t.Fatalf("lost typed schema: %#v", result)
		}
		if got := fmt.Sprint(queryRows(t, db, result)); got != `[[{"x":1} 1 x' OR 1=1 --]]` {
			t.Fatal(got)
		}
	}
}

func TestMaterializeSingleEvaluation(t *testing.T) {
	var count atomic.Int64
	driver := "materialize_" + t.Name()
	sql.Register(driver, &sqlite3.SQLiteDriver{ConnectHook: func(c *sqlite3.SQLiteConn) error {
		return c.RegisterFunc("test_tick", func() int64 { return count.Add(1) }, false)
	}})
	db, err := sql.Open(driver, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tick := ksql.WithTypedFunction("tick", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		return ksql.TypedExpression{SQL: &sqlast.Call{Name: "test_tick"}, Type: ksql.TypeLong}, nil
	})
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), tick, ksql.WithParameterization(mode))
		for _, tc := range []struct {
			query, want string
			calls       int64
		}{
			{`let T=materialize(datatable(n:long)[1,2,3]|project n,x=tick()); union T,T|order by n asc,x asc`, `[[1 1] [1 1] [2 2] [2 2] [3 3] [3 3]]`, 3},
			{`let T=materialize(datatable(n:long)[1,2,3]|project n,x=tick()); T|join kind=inner (T) on n|project n,x,x1|order by n asc`, `[[1 1 1] [2 2 2] [3 3 3]]`, 3},
			{`datatable(n:long)[1,2,3]|extend x=tick()|as hint.materialized=true T|union T|order by n asc,x asc`, `[[1 1] [1 1] [2 2] [2 2] [3 3] [3 3]]`, 3},
			{`let T=materialize(print x=tick()); let U=materialize(T|extend y=tick()); union U,U`, `[[1 2] [1 2]]`, 2},
			{`let T=materialize(datatable(n:long)[1,2,3]|project x=tick()); let total=toscalar(T|summarize sum(x)); T|extend total=total`, `[[1 6] [2 6] [3 6]]`, 3},
			{`let n=toscalar(tick()); let T=materialize(datatable(x:long)[1,2]|extend n=n,y=tick()); union T,T`, `[[1 1 2] [2 1 3] [1 1 2] [2 1 3]]`, 3},
			{`let unused=materialize(print x=tick()); print 7`, `[[7]]`, 0},
			{`let f=(){materialize(print x=tick())}; print 7`, `[[7]]`, 0},
			{`let f=(){materialize(print x=tick())}; union f(),f()`, `[[1] [2]]`, 2},
			{`let f=(T:(*)){materialize(T)}; let T=f((print x=tick())); union T,T`, `[[1] [1]]`, 1},
			{`let T=datatable(n:long)[1,2]|project x=tick(); union T,T`, `[[1] [2] [3] [4]]`, 4},
			{`datatable(n:long)[1,2]|project x=tick()|as hint.materialized=false T|union T`, `[[1] [2] [3] [4]]`, 4},
		} {
			result := compiler.Compile(tc.query)
			if !result.OK() {
				t.Fatalf("%s: %#v", tc.query, result.Diagnostics)
			}
			for execution := 0; execution < 2; execution++ {
				count.Store(0)
				got := fmt.Sprint(queryRows(t, db, result))
				if got != tc.want || count.Load() != tc.calls {
					t.Fatalf("mode=%d %s: got=%s calls=%d want=%s calls=%d SQL=%s", mode, tc.query, got, count.Load(), tc.want, tc.calls, result.SQL)
				}
			}
		}
	}
}

func TestMaterializeInvalidFormsAndCorrelation(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()))
	for _, query := range []string{
		`materialize()`, `materialize(1)`, `materialize(Events,Events)`, `materialize(T=Events)`, `print materialize(Events)`, `materialize(Events|)`, `materialize(Events|count`,
		`let f=(x:long){materialize(print n=x)}; datatable(n:long)[1]|mv-apply y=dynamic([1]) on (invoke f(n))`,
		`let f=(x:long){toscalar(materialize(print n=x))}; datatable(x:long)[1]|project f(x)`,
		`let f=(x:long){let T=datatable(n:long)[1]|extend n=x; toscalar(materialize(T))}; datatable(x:long)[1]|project f(x)`,
		`let f=(x:long){let T=datatable(n:long)[1]|extend n=x; let U=T; toscalar(materialize(U))}; datatable(x:long)[1]|project f(x)`,
		`let f=(x:long){let hidden=(){x};toscalar(materialize(print n=hidden()))};datatable(x:long)[1]|project f(x)`,
		`let f=(x:long){let T=datatable(n:long)[1]|extend n=x|as hint.materialized=true U;toscalar(U)};datatable(x:long)[1]|project f(x)`,
	} {
		result := compiler.Compile(query)
		if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Span.Line < 1 {
			t.Fatalf("accepted %s: %#v", query, result)
		}
	}
	for _, target := range []dialect.Dialect{dialect.ANSI(), dialect.MySQL(), dialect.SQLServer()} {
		for _, query := range []string{`materialize(print n=1)`, `print n=1|as hint.materialized=true T`} {
			result := ksql.New(target).Compile(query)
			if result.OK() || !strings.Contains(fmt.Sprint(result.Diagnostics), "materialized bindings") {
				t.Fatalf("%s: %#v", target.Name(), result)
			}
		}
	}
	// Unknown schemas are retained; caching does not require knowing the first column.
	result := ksql.New(dialect.SQLite()).Compile(`materialize(Events)`)
	if !result.OK() {
		t.Fatal(result.Diagnostics)
	}
}

func TestMaterializedBindingsNamesParametersAndLimits(t *testing.T) {
	db := openInvestigationDB(t)
	if _, err := db.Exec(`CREATE TABLE "__KSQL_TABLE_0" (x INTEGER);INSERT INTO "__KSQL_TABLE_0" VALUES (7)`); err != nil {
		t.Fatal(err)
	}
	catalog := testCatalog{"T": {Name: "__KSQL_TABLE_0", Schema: ksql.Schema{Columns: []ksql.Column{{Name: "x", Type: ksql.TypeLong}}}}}
	result := ksql.New(dialect.SQLite(), ksql.WithCatalog(catalog)).Compile(`let C=materialize(T); union C,T|order by x asc`)
	if got := fmt.Sprint(queryRows(t, db, result)); got != `[[7] [7]]` {
		t.Fatal(got)
	}
	if strings.Contains(result.SQL, `"__ksql_table_0" AS MATERIALIZED`) {
		t.Fatal("binding shadows physical table")
	}
	result = ksql.New(dialect.PostgreSQL(), ksql.WithParameters()).Compile(`let unused=materialize(print x=99);let n=toscalar(4);let T=materialize(print x=n+2);union T,T|where x>3`)
	if !result.OK() || fmt.Sprint(result.Args) != `[4 2 3]` || strings.Contains(result.SQL, "$4") || strings.Count(result.SQL, "AS MATERIALIZED") != 2 {
		t.Fatalf("Postgres bindings/args: %#v", result)
	}
	var query strings.Builder
	for i := 0; i < 1025; i++ {
		fmt.Fprintf(&query, "let T%d=materialize(print x=%d);", i, i)
	}
	query.WriteString("print 1")
	result = ksql.New(dialect.SQLite()).Compile(query.String())
	if result.OK() || result.SQL != "" || !strings.Contains(fmt.Sprint(result.Diagnostics), "binding work limit") {
		t.Fatalf("unbounded table bindings: %#v", result.Diagnostics)
	}
}
