package ksql_test

import (
	"database/sql"
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"strings"
	"testing"
)

func TestSQLiteGetSchemaAndDatatable(t *testing.T) {
	db, err := sql.Open(sqliteTestDriver, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	compiler := ksql.New(dialect.SQLite(), ksql.WithParameters())
	for _, tc := range []struct{ query, want string }{
		{`datatable(x:long,y:string)[1,"a",2,"b"] | order by x asc`, `[[1 a] [2 b]]`},
		{`datatable(x:long)[] | count`, `[[0]]`},
		{`datatable(x:long)[] | getschema`, `[[x 0 System.Int64 long]]`},
		{`print a=1,b="two",c=true,d=1.5,e=int(1) | getschema`, `[[a 0 System.Int64 long] [b 1 System.String string] [c 2 System.Boolean bool] [d 3 System.Double real] [e 4 System.Int32 int]]`},
		{`print a=1,b="two" | project-rename Name=b | project-reorder Name | getschema | project ColumnName,ColumnOrdinal`, `[[Name 0] [a 1]]`},
		{`print a=1 | where a == 2 | getschema | count`, `[[1]]`},
		{`print a=1 | getschema | getschema | project ColumnName,ColumnType`, `[[ColumnName string] [ColumnOrdinal long] [DataType string] [ColumnType string]]`},
		{`print a=1 | getschema | where ColumnType == "long" | project ColumnName`, `[[a]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			result := compiler.Compile(tc.query)
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.want {
				t.Fatalf("got %s want %s SQL=%s", got, tc.want, result.SQL)
			}
		})
	}
	// The physical table need not exist: schema introspection does not read rows.
	catalog := testCatalog{"T": {Name: "nonexistent", Schema: ksql.Schema{Columns: []ksql.Column{{Name: "quote'", Type: ksql.TypeString}, {Name: "D", Type: ksql.TypeDynamic}, {Name: "Time", Type: ksql.TypeDateTime}}}}}
	result := ksql.New(dialect.SQLite(), ksql.WithCatalog(catalog), ksql.WithParameters()).Compile(`T | where D == "unused" | getschema`)
	if got := fmt.Sprint(queryRows(t, db, result)); got != `[[quote' 0 System.String string] [D 1 System.Object dynamic] [Time 2 System.DateTime datetime]]` {
		t.Fatal(got)
	}
	for _, arg := range result.Args {
		if arg == "unused" {
			t.Fatal("discarded input parameter leaked")
		}
	}
}

func TestGetSchemaDiagnosticsAndDialects(t *testing.T) {
	for _, query := range []string{`T | getschema`, `print x=null | getschema`, `print a=1 | getschema kind=csl`, `print a=1 | getschema unexpected`} {
		result := ksql.New(dialect.SQLite()).Compile(query)
		if result.OK() || result.SQL != "" {
			t.Fatalf("accepted %s: %#v", query, result)
		}
	}
	for _, target := range []dialect.Dialect{dialect.SQLite(), dialect.PostgreSQL(), dialect.MySQL(), dialect.SQLServer()} {
		result := ksql.New(target, ksql.WithParameters()).Compile(`print a=1 | getschema`)
		if !result.OK() || !strings.Contains(result.SQL, "ColumnName") {
			t.Fatalf("%s: %#v", target.Name(), result)
		}
	}
}

func TestGetSchemaEmptyAndWideCatalog(t *testing.T) {
	db, err := sql.Open(sqliteTestDriver, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, width := range []int{0, 600} {
		schema := ksql.Schema{}
		for i := 0; i < width; i++ {
			schema.Columns = append(schema.Columns, ksql.Column{Name: fmt.Sprintf("C%d", i), Type: ksql.TypeLong})
		}
		compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(testCatalog{"T": {Name: "unused", Schema: schema}}), ksql.WithParameters())
		result := compiler.Compile(`T | getschema | count`)
		if got := fmt.Sprint(queryRows(t, db, result)); got != fmt.Sprintf("[[%d]]", width) {
			t.Fatalf("width=%d got=%s", width, got)
		}
	}
}
