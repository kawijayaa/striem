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

func TestArgMinMaxValuesAndSchemas(t *testing.T) {
	db := openInvestigationDB(t)
	source := `datatable(g:string,n:long,label:string)["a",1,"first","a",3,"last","b",2,"only"]`
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode))
		for _, tc := range []struct {
			query, want string
			names       []string
		}{
			{source + `|summarize arg_max(n,*) by g|order by g asc`, `[[a 3 last] [b 2 only]]`, []string{"g", "n", "label"}},
			{source + `|summarize arg_min(n,*) by g|order by g asc`, `[[a 1 first] [b 2 only]]`, []string{"g", "n", "label"}},
			{source + `|summarize latest=arg_max(n,label),earliest=arg_min(n,label),total=count(),sum=sum(n) by g|order by g asc`, `[[a 3 last 1 first 2 4] [b 2 only 2 only 1 2]]`, []string{"g", "latest", "label", "earliest", "label1", "total", "sum"}},
			{source + `|summarize (latest,last_label)=arg_max(n,label),(earliest,first_label)=arg_min(n,label),total=count() by g|order by g asc`, `[[a 3 last 1 first 2] [b 2 only 2 only 1]]`, []string{"g", "latest", "last_label", "earliest", "first_label", "total"}},
			{source + `|summarize (['latest.n'])=arg_max(n,label)`, `[[3 last]]`, []string{"latest.n", "label"}},
			{source + `|summarize arg_max(n+2,n+1)`, `[[5 4]]`, []string{"max_", "max__arg1"}},
			{source + `|summarize argmax(n+2,label)`, `[[5 last]]`, []string{"max_", "max__label"}},
			{source + `|summarize arg_max(n),arg_min(n)`, `[[3 1]]`, []string{"n", "n1"}},
			{source + `|summarize arg_max(n,n,label)`, `[[3 3 last]]`, []string{"n", "n1", "label"}},
			{source + `|summarize argmax(n,label),argmin(n,label)`, `[[3 last 1 first]]`, []string{"max_n", "max_n_label", "min_n", "min_n_label"}},
			{source + `|summarize latest=arg_max(n,label,n,*),earliest=arg_min(n,*) by g|order by g asc`, `[[a 3 last 1 first] [b 2 only 2 only]]`, []string{"g", "latest", "label", "earliest", "label1"}},
			{source + `|summarize arg_max(n+2,label) by grp=strcat(g,"!")|order by grp asc`, `[[a! 5 last] [b! 4 only]]`, []string{"grp", "max_", "label"}},
			{source + `|summarize arg_max(n,label,strcat(label,"!"))`, `[[3 last last!]]`, []string{"n", "label", "strcat_"}},
			{`datatable(n:long,label:string)[]|summarize latest=arg_max(n,label),earliest=arg_min(n,label),total=count()`, `[[<nil> <nil> <nil> <nil> 0]]`, []string{"latest", "label", "earliest", "label1", "total"}},
			{`datatable(g:string,n:long,label:string)[]|summarize arg_max(n,*) by g`, `[]`, []string{"g", "n", "label"}},
			{`datatable(n:long,label:string)[long(null),"null",2,"two",1,"one"]|summarize arg_min(n,label),arg_max(n,label)`, `[[1 one 2 two]]`, []string{"n", "label", "n1", "label1"}},
			{`datatable(g:long,n:long,label:string)[long(null),1,"one",long(null),2,"two",1,3,"three"]|summarize arg_max(n,label),total=count() by g|order by g asc nulls first`, `[[<nil> 2 two 2] [1 3 three 1]]`, []string{"g", "n", "label", "total"}},
			{`let T=materialize(` + source + `); T|summarize arg_max(n,*) by g|join kind=inner (T) on g|project g,n,n1|order by g asc,n1 asc`, `[[a 3 1] [a 3 3] [b 2 2]]`, []string{"g", "n", "n1"}},
			{`let f=(T:(g:string,n:long,label:string)){T|summarize arg_max(n,*) by g}; f(` + source + `)|order by g asc`, `[[a 3 last] [b 2 only]]`, []string{"g", "n", "label"}},
		} {
			result := compiler.Compile(tc.query)
			if !result.OK() {
				t.Fatalf("%s: %#v", tc.query, result.Diagnostics)
			}
			names := []string{}
			for _, column := range result.Columns {
				names = append(names, column.Name)
			}
			if !reflect.DeepEqual(names, tc.names) {
				t.Fatalf("%s: names=%v want=%v", tc.query, names, tc.names)
			}
			rows, err := db.Query(result.SQL, result.Args...)
			if err != nil {
				t.Fatalf("%s SQL=%s args=%v: %v", tc.query, result.SQL, result.Args, err)
			}
			actual, err := rows.Columns()
			rows.Close()
			if err != nil || !reflect.DeepEqual(actual, tc.names) {
				t.Fatalf("actual SQL schema=%v want=%v err=%v", actual, tc.names, err)
			}
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.want {
				t.Fatalf("mode=%d %s: got=%s want=%s SQL=%s args=%v", mode, tc.query, got, tc.want, result.SQL, result.Args)
			}
		}
	}
}

func TestArgMinMaxTieAndAllNullRowsAreCoherent(t *testing.T) {
	db := openInvestigationDB(t)
	for _, fn := range []string{"arg_min", "arg_max", "argmin", "argmax"} {
		for _, values := range []string{`1,"a",10,1,"b",20`, `long(null),"a",10,long(null),"b",20`} {
			result := ksql.New(dialect.SQLite()).Compile(`datatable(n:long,label:string,other:long)[` + values + `]|summarize ` + fn + `(n,*)`)
			rows := queryRows(t, db, result)
			if len(rows) != 1 || len(rows[0]) != 3 {
				t.Fatalf("rows=%#v", rows)
			}
			if (rows[0][1] != "a" || rows[0][2] != int64(10)) && (rows[0][1] != "b" || rows[0][2] != int64(20)) {
				t.Fatalf("mixed rows=%#v", rows)
			}
		}
	}
}

func TestArgMinMaxPayloadTypesAndInternalNames(t *testing.T) {
	db := openInvestigationDB(t)
	compiler := ksql.New(dialect.SQLite())
	result := compiler.Compile(`datatable(n:long,['a.b']:string,__ksql_selection_0:long,d:dynamic,b:bool)[1,"first",20,dynamic({"x":1}),true,2,"last",30,dynamic([1,true]),false]|summarize arg_max(n,*)`)
	if !result.OK() || len(result.Columns) != 5 || result.Columns[3].Type != ksql.TypeDynamic || result.Columns[4].Type != ksql.TypeBool {
		t.Fatalf("schema: %#v", result)
	}
	if got := fmt.Sprint(queryRows(t, db, result)); got != `[[2 last 30 [1,true] 0]]` {
		t.Fatal(got)
	}
	result = compiler.Compile(`datatable(n:long,s:string,d:dynamic,b:bool)[1,"x",dynamic({"ok":true}),null,2,"y",dynamic(null),false]|summarize arg_min(n,*)`)
	if got := fmt.Sprint(queryRows(t, db, result)); got != `[[1 x {"ok":true} <nil>]]` {
		t.Fatal(got)
	}
	result = compiler.Compile(`datatable(n:long,s:string)[9223372036854775807,"big",9223372036854775806,"small"]|summarize arg_max(n,s),arg_min(n,s)`)
	if got := fmt.Sprint(queryRows(t, db, result)); got != `[[9223372036854775807 big 9223372036854775806 small]]` {
		t.Fatal(got)
	}
}

func TestArgSelectionPrecomputesNondeterministicKeys(t *testing.T) {
	var count atomic.Int64
	driver := "arg_selection_" + t.Name()
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
	compiler := ksql.New(dialect.SQLite(), tick)
	for _, tc := range []struct {
		query, want string
		calls       int64
	}{
		{`datatable(n:long)[1,2,3]|summarize arg_max(tick(),n)`, `[[3 3]]`, 3},
		{`datatable(n:long)[1,2,3]|summarize arg_min(tick(),n)`, `[[1 1]]`, 3},
		{`datatable(n:long)[1,2,3]|extend x=tick()|summarize arg_max(x,n),total=sum(x)`, `[[3 3 6]]`, 3},
		{`datatable(n:long)[1,2,3]|summarize arg_max(n,tick())`, `[[3 3]]`, 3},
	} {
		result := compiler.Compile(tc.query)
		for execution := 0; execution < 2; execution++ {
			count.Store(0)
			got := fmt.Sprint(queryRows(t, db, result))
			if got != tc.want || count.Load() != tc.calls {
				t.Fatalf("%s: got=%s calls=%d want=%s calls=%d SQL=%s", tc.query, got, count.Load(), tc.want, tc.calls, result.SQL)
			}
		}
	}
}

func TestArgSelectionErrorsLimitsAndMappings(t *testing.T) {
	for _, query := range []string{`print arg_max(1,2)`, `datatable(n:long)[1]|extend x=arg_min(n)`, `datatable(n:long)[1]|summarize arg_max()`, `datatable(n:long)[1]|summarize arg_min(*,n)`, `datatable(d:dynamic)[dynamic({})]|summarize arg_max(d,*)`, `datatable(n:long)[1]|summarize arg_max(missing,*)`, `datatable(n:long)[1]|summarize a=arg_max(n),a=arg_min(n)`, `datatable(n:long)[1]|summarize (a,b)=arg_max(n)`, `datatable(n:long)[1]|summarize (a,)=arg_max(n)`, `datatable(n:long)[1]|summarize ()=arg_max(n)`, `datatable(n:long)[1]|summarize (a,b)=arg_max(n,)`} {
		result := ksql.New(dialect.SQLite()).Compile(query)
		if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Span.Line < 1 {
			t.Fatalf("accepted %s: %#v", query, result)
		}
	}
	result := ksql.New(dialect.SQLite()).Compile(`Events|summarize arg_max(n,*)`)
	if result.OK() || !strings.Contains(fmt.Sprint(result.Diagnostics), "bound input schema") {
		t.Fatal(result.Diagnostics)
	}
	result = ksql.New(dialect.SQLite(), ksql.WithLimits(ksql.Limits{MaxProjectionItems: 2})).Compile(`datatable(a:long,b:long,c:long)[1,2,3]|summarize arg_max(a,*)`)
	if result.OK() || !strings.Contains(fmt.Sprint(result.Diagnostics), "MaxProjectionItems") {
		t.Fatal(result.Diagnostics)
	}
	for _, target := range []dialect.Dialect{dialect.ANSI(), dialect.MySQL(), dialect.SQLServer()} {
		result := ksql.New(target).Compile(`print n=1|summarize arg_max(n)`)
		if result.OK() || result.SQL != "" {
			t.Fatalf("%s: %#v", target.Name(), result)
		}
	}
	// Explicit scalar extensions keep their existing precedence.
	result = ksql.New(dialect.SQLite(), ksql.WithFunction("arg_max", ksql.SQLFunction("max"))).Compile(`datatable(n:long)[1,2]|summarize latest=arg_max(n)`)
	if got := fmt.Sprint(queryRows(t, openInvestigationDB(t), result)); got != `[[2]]` {
		t.Fatal(got)
	}
	result = ksql.New(dialect.PostgreSQL(), ksql.WithParameters()).Compile(`datatable(n:long,b:bool)[1,true,2,false]|summarize arg_max(n,b),total=count()`)
	if !result.OK() || !strings.Contains(result.SQL, "ARRAY_AGG(") || !strings.Contains(result.SQL, "AS MATERIALIZED") || fmt.Sprint(result.Args) != `[1 true 2 false]` {
		t.Fatalf("Postgres SQL/args: %#v", result)
	}
}

func TestArgMinMaxDocumentedNullExample(t *testing.T) {
	db := openInvestigationDB(t)
	source := `datatable(Fruit:string,Color:string,Version:int)["Apple","Red",1,"Apple","Green",int(null),"Banana","Yellow",int(null),"Banana","Green",int(null),"Pear","Brown",1,"Pear","Green",2]`
	for _, fn := range []string{"arg_max", "arg_min"} {
		result := ksql.New(dialect.SQLite()).Compile(source + `|summarize ` + fn + `(Version,*) by Fruit|order by Fruit asc`)
		rows := queryRows(t, db, result)
		if len(rows) != 3 || fmt.Sprint(rows[0]) != `[Apple 1 Red]` || rows[1][0] != "Banana" || rows[1][1] != nil || (rows[1][2] != "Yellow" && rows[1][2] != "Green") {
			t.Fatalf("%s: %#v", fn, rows)
		}
		want := `[Pear 2 Green]`
		if fn == "arg_min" {
			want = `[Pear 1 Brown]`
		}
		if fmt.Sprint(rows[2]) != want {
			t.Fatalf("%s: %#v", fn, rows)
		}
		if result.Columns[1].Type != ksql.TypeInt {
			t.Fatal(result.Columns)
		}
	}
}

func TestArgMinMaxDatesBooleansAndQuotedAliases(t *testing.T) {
	db := openInvestigationDB(t)
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()))
	result := compiler.Compile(`Events|summarize (['last.time'],['last.host'])=arg_max(TimeGenerated,Host),(['first.time'],['first.host'])=arg_min(TimeGenerated,Host)`)
	if got := fmt.Sprint(queryRows(t, db, result)); got != `[[2026-07-28T10:00:00Z alpha 2026-07-28T09:00:00Z beta]]` {
		t.Fatal(got)
	}
	if result.Columns[0].Type != ksql.TypeDateTime || result.Columns[2].Type != ksql.TypeDateTime {
		t.Fatal(result.Columns)
	}
	result = compiler.Compile(`datatable(n:long,b:bool)[1,false,2,true,3,null]|summarize arg_max(b,n),arg_min(b,n)`)
	if got := fmt.Sprint(queryRows(t, db, result)); got != `[[1 2 0 1]]` {
		t.Fatal(got)
	}
	if result.Columns[0].Type != ksql.TypeBool || result.Columns[2].Type != ksql.TypeBool {
		t.Fatal(result.Columns)
	}
	result = compiler.Compile(`datatable(n:long,s:string)[1,"a",2,"b"]|summarize arg_max(['score']=n,['label']=s)`)
	if got := fmt.Sprint(queryRows(t, db, result)); got != `[[2 b]]` {
		t.Fatal(got)
	}
	if result.Columns[0].Name != "score" || result.Columns[1].Name != "label" {
		t.Fatal(result.Columns)
	}
}

func TestArgSelectionArgumentPolicyBoundary(t *testing.T) {
	var query strings.Builder
	query.WriteString(`print n=1|summarize arg_max(n`)
	for i := 1; i < 128; i++ {
		fmt.Fprintf(&query, ",%d", i)
	}
	query.WriteString(")")
	compiler := ksql.New(dialect.SQLite(), ksql.WithParameters())
	result := compiler.Compile(query.String())
	rows := queryRows(t, openInvestigationDB(t), result)
	if len(result.Columns) != 128 || len(rows) != 1 || len(rows[0]) != 128 || rows[0][127] != int64(127) {
		t.Fatalf("128 arguments: cols=%d rows=%v", len(result.Columns), rows)
	}
	excessive := strings.TrimSuffix(query.String(), ")") + ",128)"
	result = compiler.Compile(excessive)
	if result.OK() || result.SQL != "" || !strings.Contains(fmt.Sprint(result.Diagnostics), "128") {
		t.Fatalf("unbounded arguments: %#v", result.Diagnostics)
	}
}
