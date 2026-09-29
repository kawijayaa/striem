package dialect

import (
	"testing"

	"github.com/kawijayaa/ksql/sqlast"
)

func TestRenderNestedSelect(t *testing.T) {
	query := &sqlast.Select{
		Projections: []sqlast.SelectItem{{Expr: &sqlast.Identifier{Parts: []string{"x"}}}},
		From: &sqlast.Subquery{
			Query: &sqlast.Select{From: &sqlast.Table{Parts: []string{"Events"}}},
			Alias: "_k0",
		},
		Where: &sqlast.Binary{
			Left: &sqlast.Identifier{Parts: []string{"x"}}, Operator: ">",
			Right: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "1"},
		},
		Limit: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "5"},
	}
	got, err := Render(PostgreSQL(), query)
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT "x" FROM (SELECT * FROM "Events") AS "_k0" WHERE ("x" > 1) LIMIT 5`
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestDialectSyntax(t *testing.T) {
	if got := SQLServer().QuoteIdentifier("a]b"); got != "[a]]b]" {
		t.Errorf("SQL Server quote = %q", got)
	}
	if got := MySQL().QuoteIdentifier("a`b"); got != "`a``b`" {
		t.Errorf("MySQL quote = %q", got)
	}
	if got := PostgreSQL().Placeholder(2); got != "$2" {
		t.Errorf("PostgreSQL placeholder = %q", got)
	}
}

func TestRenderSQLServerJoinKeys(t *testing.T) {
	query := &sqlast.Select{From: &sqlast.Join{
		Left:  &sqlast.Subquery{Query: &sqlast.Select{From: &sqlast.Table{Parts: []string{"T"}}}, Alias: "l"},
		Right: &sqlast.Subquery{Query: &sqlast.Select{From: &sqlast.Table{Parts: []string{"U"}}}, Alias: "r"},
		Kind:  "INNER", Using: []string{"key"},
	}}
	got, err := Render(SQLServer(), query)
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT * FROM (SELECT * FROM [T]) AS [l] INNER JOIN (SELECT * FROM [U]) AS [r] ON [l].[key] = [r].[key]`
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRenderNullOrderingFallback(t *testing.T) {
	query := &sqlast.Select{From: &sqlast.Table{Parts: []string{"T"}}, OrderBy: []sqlast.Order{{
		Expr: &sqlast.Identifier{Parts: []string{"x"}}, Direction: "asc", Nulls: "last",
	}}}
	got, err := Render(MySQL(), query)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT * FROM `T` ORDER BY CASE WHEN `x` IS NULL THEN 1 ELSE 0 END ASC, `x` ASC"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestRandomExpressions(t *testing.T) {
	tests := []struct {
		name string
		d    Dialect
		want string
	}{
		{name: "postgresql", d: PostgreSQL(), want: "RANDOM()"},
		{name: "sqlite", d: SQLite(), want: "RANDOM()"},
		{name: "mysql", d: MySQL(), want: "RAND()"},
		{name: "sqlserver", d: SQLServer(), want: "NEWID()"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Render(test.d, &sqlast.Select{From: &sqlast.Table{Parts: []string{"T"}}, OrderBy: []sqlast.Order{{Expr: &sqlast.Random{}}}})
			if err != nil {
				t.Fatal(err)
			}
			want := "SELECT * FROM " + test.d.QuoteIdentifier("T") + " ORDER BY " + test.want
			if got != want {
				t.Fatalf("Render() = %q, want %q", got, want)
			}
		})
	}
}

func TestSQLiteJSONEach(t *testing.T) {
	query := &sqlast.Select{From: &sqlast.Join{
		Left: &sqlast.Table{Parts: []string{"T"}, Alias: "t"},
		Right: &sqlast.JSONEach{
			Value: &sqlast.Identifier{Parts: []string{"t", "items"}},
			Alias: "j",
		},
		Kind: "CROSS",
	}}
	got, err := Render(SQLite(), query)
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT * FROM "T" AS "t" CROSS JOIN json_each(CASE WHEN "t"."items" IS NULL THEN json_array(NULL) ELSE "t"."items" END) AS "j"`
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestSQLiteJSONIndexSyntax(t *testing.T) {
	nested := &sqlast.Index{
		Value: &sqlast.Index{
			Value: &sqlast.Index{
				Value: &sqlast.Identifier{Parts: []string{"d"}},
				Index: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "process"},
			},
			Index: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "command line"},
		},
		Index: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "image-name"},
	}
	query := &sqlast.Select{Projections: []sqlast.SelectItem{
		{Expr: &sqlast.Index{Value: &sqlast.Identifier{Parts: []string{"d"}}, Index: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "0"}}},
		{Expr: &sqlast.Index{Value: &sqlast.Identifier{Parts: []string{"d"}}, Index: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "a.b"}}},
		{Expr: &sqlast.Index{Value: &sqlast.Identifier{Parts: []string{"d"}}, Index: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "space key"}}},
		{Expr: &sqlast.Index{Value: &sqlast.Identifier{Parts: []string{"d"}}, Index: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "hyphen-key"}}},
		{Expr: &sqlast.Index{Value: &sqlast.Identifier{Parts: []string{"d"}}, Index: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "it's"}}},
		{Expr: &sqlast.Index{Value: &sqlast.Identifier{Parts: []string{"d"}}, Index: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: `a.b"\c`}}},
		{Expr: nested},
		{Expr: &sqlast.Index{Value: &sqlast.Identifier{Parts: []string{"d"}}, Index: &sqlast.Parameter{Index: 1, Kind: sqlast.StringLiteral}}},
	}}
	got, err := Render(SQLite(), query)
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT json_extract("d", '$[' || 0 || ']'), json_extract("d", '$."a.b"'), json_extract("d", '$."space key"'), json_extract("d", '$."hyphen-key"'), json_extract("d", '$."it''s"'), (SELECT value FROM json_each("d") WHERE key = 'a.b"\c' LIMIT 1), json_extract("d", '$."process"."command line"."image-name"'), (SELECT value FROM json_each("d") WHERE key = ? LIMIT 1)`; got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestSQLiteRegexFunction(t *testing.T) {
	query := &sqlast.Select{Projections: []sqlast.SelectItem{{Expr: &sqlast.Regex{
		Value: &sqlast.Identifier{Parts: []string{"message"}}, Pattern: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "term"},
	}}}}
	got, err := Render(SQLite(WithRegexFunction("kql_regex")), query)
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT kql_regex(lower('term'), lower("message"))`; got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
	got, err = Render(SQLite(WithRegexFunction("kql_regex"), WithRegexCaseInsensitiveFlag("(?i)")), query)
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT kql_regex('(?i)' || 'term', "message")`; got != want {
		t.Fatalf("Render() with case-insensitive flag = %q, want %q", got, want)
	}
	query.Projections[0].Expr.(*sqlast.Regex).CaseSensitive = true
	got, err = Render(SQLite(WithRegexFunction("kql_regex"), WithRegexCaseInsensitiveFlag("(?i)")), query)
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT kql_regex('term', "message")`; got != want {
		t.Fatalf("Render() case-sensitive = %q, want %q", got, want)
	}
	if _, err := Render(SQLite(WithRegexFunction("bad();drop")), query); err == nil {
		t.Fatal("invalid regex function name was accepted")
	}
}
