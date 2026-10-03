package compiler_test

import (
	"fmt"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestDirectTabularLetAliases(t *testing.T) {
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()), ksql.WithParameterization(mode))
		for _, tc := range []struct{ query, rows string }{
			{`let T=Events; T | project Host | order by Host asc`, `[[alpha] [beta]]`},
			{`let T=(Events); let U=((T)); U | project Host | order by Host asc`, `[[alpha] [beta]]`},
			{`let ['T U']=Events; let V=['T U']; V | project Host | order by Host asc`, `[[alpha] [beta]]`},
			{`let T=datatable(x:long)[1,2]; let U=T; U | union T | summarize n=count()`, `[[4]]`},
			{`let T=print x=1,d=dynamic([2]),b=true; let U=T; U | join kind=inner T on x | project d,b`, `[[[2] 1]]`},
			{`let x=3; let y=x; print v=y`, `[[3]]`},
			{`let Events=7; let X=Events; print v=X`, `[[7]]`},
			{`let d=dynamic({"x":9}); let v=d.x; print result=v`, `[[9]]`},
		} {
			if got := fmt.Sprint(queryRows(t, db, compiler.Compile(tc.query))); got != tc.rows {
				t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.rows)
			}
		}
		result := compiler.Compile(`let T=print d=dynamic([1]),b=true; let U=T; U`)
		if !result.OK() || len(result.Columns) != 2 || result.Columns[0].Type != ksql.TypeDynamic || result.Columns[1].Type != ksql.TypeBool {
			t.Fatalf("alias lost types: %#v", result)
		}
	}
	// Open-schema compilation without a catalog follows normal table sources.
	result := ksql.New(dialect.SQLite()).Compile(`let T=events; let U=T; U | project Host | order by Host asc`)
	if got := fmt.Sprint(queryRows(t, db, result)); got != `[[alpha] [beta]]` {
		t.Fatalf("open-schema alias rows=%s", got)
	}
	result = ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog())).Compile(`let T=MissingTable; T`)
	if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Span.Length == 0 {
		t.Fatalf("unknown table accepted: %#v", result)
	}
	catalog := testCatalog{"table.with.dots": {Name: "events", Schema: investigationCatalog()["Events"].Schema}}
	result = ksql.New(dialect.SQLite(), ksql.WithCatalog(catalog)).Compile(`let T=['table.with.dots']; T | project Host | order by Host asc`)
	if got := fmt.Sprint(queryRows(t, db, result)); got != `[[alpha] [beta]]` {
		t.Fatalf("quoted table alias rows=%s", got)
	}
}

func TestDuplicateLetDeclarationsRejected(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()))
	for _, query := range []string{
		`let x=1; let x=2; print v=x`,
		`let T=Events; let T=Events; T`,
		`let x=1; let x=Events; x`,
		`let x=Events; let x=1; print v=x`,
		`let ['x']=1; let x=2; print v=x`,
	} {
		result := compiler.Compile(query)
		if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "KQLB0101" || result.Diagnostics[0].Span.Length == 0 {
			t.Fatalf("duplicate declaration accepted: %#v", result)
		}
	}
}
