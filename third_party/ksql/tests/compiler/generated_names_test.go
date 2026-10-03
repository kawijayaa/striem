package compiler_test

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"reflect"
	"testing"
)

func TestGeneratedProjectionAndExtendNames(t *testing.T) {
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode))
		for _, tc := range []struct {
			query, rows string
			names       []string
		}{
			{`print x=1 | extend x+1,x+2 | project Column1,Column2`, `[[2 3]]`, []string{"Column1", "Column2"}},
			{`print Column1=5 | extend 10 | project Column1,Column2`, `[[5 10]]`, []string{"Column1", "Column2"}},
			{`print Column1=5 | project Column1+1`, `[[6]]`, []string{"Column1"}},
			{`let Column1=99; print x=1 | extend 2 | project Column2`, `[[2]]`, []string{"Column2"}},
			{`let Column1=print x=99; print x=1 | extend 2 | project Column2`, `[[2]]`, []string{"Column2"}},
			{`let Column1=99; print x=1 | project 2,3`, `[[2 3]]`, []string{"Column2", "Column3"}},
			{`print x=1 | project named=x,x+1,x+2`, `[[1 2 3]]`, []string{"named", "Column1", "Column2"}},
			{`print x=1 | extend 3 | project 4`, `[[4]]`, []string{"Column2"}},
			{`print x=1 | extend tostring(17) | project Column1`, `[[17]]`, []string{"Column1"}},
			{`print x=1 | extend dynamic([1,2]) | project Column1`, `[[[1,2]]]`, []string{"Column1"}},
		} {
			result := compiler.Compile(tc.query)
			if !result.OK() {
				t.Fatalf("%s: %#v", tc.query, result.Diagnostics)
			}
			var names []string
			for _, c := range result.Columns {
				names = append(names, c.Name)
			}
			if !reflect.DeepEqual(names, tc.names) {
				t.Fatalf("%s names=%v want=%v", tc.query, names, tc.names)
			}
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.rows {
				t.Fatalf("%s rows=%s want=%s", tc.query, got, tc.rows)
			}
		}
	}
}

func TestGeneratedNamesAvoidCatalogAndBoundSearch(t *testing.T) {
	catalog := testCatalog{"Column1": {Name: "reserved"}}
	result := ksql.New(dialect.SQLite(), ksql.WithCatalog(catalog)).Compile(`print x=1 | extend 2 | project Column2`)
	if !result.OK() || len(result.Columns) != 1 || result.Columns[0].Name != "Column2" {
		t.Fatal(result)
	}
	allNames := ksql.CatalogFunc(func(name string) (ksql.Table, bool) { return ksql.Table{Name: name}, true })
	result = ksql.New(dialect.SQLite(), ksql.WithCatalog(allNames)).Compile(`print x=1 | extend 2`)
	if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "KQLB0407" {
		t.Fatal(result)
	}
}
