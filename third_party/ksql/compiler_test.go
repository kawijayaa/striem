package ksql_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/features"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

func TestCompilePipeline(t *testing.T) {
	compiler := ksql.New(dialect.PostgreSQL())
	result := compiler.Compile(`let cutoff = 10;
Events
| where severity >= cutoff and message contains "error"
| extend day = floor(duration)
| summarize total = count(), average = avg(duration) by service
| sort by total desc
| take 5`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := `SELECT * FROM (SELECT * FROM (SELECT "service" AS "service", COUNT(*) AS "total", AVG("duration") AS "average" FROM (SELECT *, FLOOR("duration") AS "day" FROM (SELECT * FROM (SELECT * FROM "Events") AS "_k0" WHERE (("severity" >= 10) AND (LOWER("message") LIKE LOWER(CONCAT('%', 'error', '%'))))) AS "_k1") AS "_k2" GROUP BY "service") AS "_k3" ORDER BY "total" DESC NULLS LAST) AS "_k4" LIMIT 5`
	if result.SQL != want {
		t.Fatalf("Compile() SQL =\n%s\nwant:\n%s", result.SQL, want)
	}
	for _, used := range result.Features {
		if _, ok := features.Lookup(used.ID); !ok {
			t.Errorf("runtime feature %q is absent from ledger", used.ID)
		}
	}
}

func TestCompilePrint(t *testing.T) {
	result := ksql.New(dialect.SQLite()).Compile(`print answer=40+2, label="ok"`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	if result.SQL != `SELECT (40 + 2) AS "answer", 'ok' AS "label"` {
		t.Fatalf("Compile() SQL = %q", result.SQL)
	}
}

func TestUnsupportedFeatureIsDiagnostic(t *testing.T) {
	result := ksql.New(dialect.PostgreSQL()).Compile(`T | evaluate python(typeof(*), code)`)
	if result.OK() || result.SQL != "" {
		t.Fatalf("Compile() = %#v", result)
	}
	if got := result.Diagnostics[0].Feature; got != "operator.evaluate" {
		t.Fatalf("feature = %q", got)
	}
}

func TestUnknownFunctionIsNotPassedThrough(t *testing.T) {
	result := ksql.New(dialect.PostgreSQL()).Compile(`T | project mystery(x)`)
	if result.OK() || result.SQL != "" {
		t.Fatalf("Compile() = %#v", result)
	}
	if got := result.Diagnostics[0].Code; got != "KQLL0503" {
		t.Fatalf("code = %q", got)
	}
}

func TestCompileJoin(t *testing.T) {
	result := ksql.New(dialect.PostgreSQL()).Compile(`T | join kind=inner (U | where active == true) on key`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := `SELECT * FROM (SELECT * FROM "T") AS "_k0" INNER JOIN (SELECT * FROM (SELECT * FROM "U") AS "_k1" WHERE ("active" = TRUE)) AS "_k2" ON ("_k0"."key" = "_k2"."key")`
	if result.SQL != want {
		t.Fatalf("Compile() SQL = %q, want %q", result.SQL, want)
	}
}

func TestCompileQualifiedJoin(t *testing.T) {
	result := ksql.New(dialect.PostgreSQL()).Compile(`T | join kind=leftouter (U) on $left.key == $right.other_key`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := `SELECT * FROM (SELECT * FROM "T") AS "_k0" LEFT JOIN (SELECT * FROM "U") AS "_k1" ON ("_k0"."key" = "_k1"."other_key")`
	if result.SQL != want {
		t.Fatalf("Compile() SQL = %q, want %q", result.SQL, want)
	}
}

func TestCompileDatatable(t *testing.T) {
	result := ksql.New(dialect.PostgreSQL()).Compile(`datatable(id:long, name:string)[1, "one", 2, "two"] | where id > 1`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := `SELECT * FROM (SELECT * FROM (VALUES (1, 'one'), (2, 'two')) AS "_k0"("id", "name")) AS "_k1" WHERE ("id" > 1)`
	if result.SQL != want {
		t.Fatalf("Compile() SQL = %q, want %q", result.SQL, want)
	}
}

func TestCompileRange(t *testing.T) {
	result := ksql.New(dialect.PostgreSQL()).Compile(`range id from 1 to 5 step 2`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := `SELECT * FROM generate_series(1, 5, 2) AS "_k0"("id")`
	if result.SQL != want {
		t.Fatalf("Compile() SQL = %q, want %q", result.SQL, want)
	}
}

func TestCompileTabularLet(t *testing.T) {
	result := ksql.New(dialect.PostgreSQL()).Compile(`let Active = Events | where active == true; Active | project id | sort by id`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := `SELECT * FROM (SELECT "id" FROM (SELECT * FROM (SELECT * FROM "Events") AS "_k0" WHERE ("active" = TRUE)) AS "_k1") AS "_k2" ORDER BY "id" DESC NULLS LAST`
	if result.SQL != want {
		t.Fatalf("Compile() SQL = %q, want %q", result.SQL, want)
	}
}

func TestCustomFunctionRule(t *testing.T) {
	compiler := ksql.New(dialect.PostgreSQL(), ksql.WithFunction("hash_sha256", ksql.SQLFunction("digest_sha256")))
	result := compiler.Compile(`T | project digest=hash_sha256(value)`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := `SELECT DIGEST_SHA256("value") AS "digest" FROM (SELECT * FROM "T") AS "_k0"`
	if result.SQL != want {
		t.Fatalf("Compile() SQL = %q, want %q", result.SQL, want)
	}
}

func TestCustomOperatorRule(t *testing.T) {
	compiler := ksql.New(dialect.PostgreSQL(), ksql.WithOperator("evaluate", func(_ ksql.LoweringContext, input ksql.Relation, operator kql.Operator) (ksql.Relation, error) {
		return ksql.Relation{Query: &sqlast.Select{From: &sqlast.Subquery{Query: input.Query, Alias: "plugin_input"}}, Schema: input.Schema}, nil
	}))
	result := compiler.Compile(`T | evaluate identity()`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := `SELECT * FROM (SELECT * FROM "T") AS "plugin_input"`
	if result.SQL != want {
		t.Fatalf("Compile() SQL = %q, want %q", result.SQL, want)
	}
}

func TestCompileSQLiteMvExpand(t *testing.T) {
	result := ksql.New(dialect.SQLite()).Compile(`Events | mv-expand with_itemindex=i item=Items limit 2`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	for _, fragment := range []string{`CROSS JOIN json_each(`, `JSON_OBJECT("_k1"."key"`, `AS "item"`, `AS "i"`} {
		if !strings.Contains(result.SQL, fragment) {
			t.Fatalf("Compile() SQL lacks %q:\n%s", fragment, result.SQL)
		}
	}
}

func TestCompileSQLiteMvApplyFilter(t *testing.T) {
	result := ksql.New(dialect.SQLite()).Compile(`Events | mv-apply item=Items on (where item > 1 | extend doubled=item*2)`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	for _, fragment := range []string{`CROSS JOIN json_each(`, `WHERE ("item" > 1)`, `AS "doubled"`} {
		if !strings.Contains(result.SQL, fragment) {
			t.Fatalf("Compile() SQL lacks %q:\n%s", fragment, result.SQL)
		}
	}
}

func TestCompileMvExpandRequiresReplacementSchema(t *testing.T) {
	result := ksql.New(dialect.SQLite()).Compile(`Events | mv-expand Items`)
	if result.OK() || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "KQLL0333" {
		t.Fatalf("Compile() = %#v", result)
	}
}

func TestCompileMvApplyRejectsNonRowwiseSubquery(t *testing.T) {
	result := ksql.New(dialect.SQLite()).Compile(`Events | mv-apply item=Items on (top 2 by item)`)
	if result.OK() || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "KQLL0334" {
		t.Fatalf("Compile() = %#v", result)
	}
}

func TestCompileSampleOperators(t *testing.T) {
	sample := ksql.New(dialect.SQLite()).Compile(`Events | sample 2`)
	if !sample.OK() || sample.SQL != `SELECT * FROM (SELECT * FROM "Events") AS "_k0" ORDER BY RANDOM() LIMIT 2` {
		t.Fatalf("sample Compile() = %#v", sample)
	}
	distinct := ksql.New(dialect.MySQL()).Compile(`Events | sample-distinct 2 of state`)
	if !distinct.OK() {
		t.Fatalf("sample-distinct diagnostics = %#v", distinct.Diagnostics)
	}
	want := "SELECT * FROM (SELECT DISTINCT `state` FROM (SELECT * FROM `Events`) AS `_k0`) AS `_k1` ORDER BY RAND() LIMIT 2"
	if distinct.SQL != want {
		t.Fatalf("sample-distinct SQL = %q, want %q", distinct.SQL, want)
	}
}

func TestCompileCaseInsensitiveMembership(t *testing.T) {
	result := ksql.New(dialect.PostgreSQL()).Compile(`Events | where state in~ ("wa", "or")`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := `SELECT * FROM (SELECT * FROM "Events") AS "_k0" WHERE (LOWER("state") IN (LOWER('wa'), LOWER('or')))`
	if result.SQL != want {
		t.Fatalf("Compile() SQL = %q, want %q", result.SQL, want)
	}
}

func TestCompileTermOperators(t *testing.T) {
	result := ksql.New(dialect.PostgreSQL()).Compile(`Events | where state has "New" and code !hasprefix_cs "ERR"`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := `SELECT * FROM (SELECT * FROM "Events") AS "_k0" WHERE (("state" ~* '(^|[^[:alnum:]])New([^[:alnum:]]|$)') AND NOT ("code" ~ '(^|[^[:alnum:]])ERR[[:alnum:]]*'))`
	if result.SQL != want {
		t.Fatalf("Compile() SQL = %q, want %q", result.SQL, want)
	}
}

func TestCompileSearchEscapesLiteralTerms(t *testing.T) {
	catalog := testCatalog{"Events": {Name: "events", Schema: ksql.Schema{Columns: []ksql.Column{{Name: "Message", Type: ksql.TypeString}}}}}
	compiler := ksql.New(dialect.PostgreSQL(), ksql.WithCatalog(catalog))
	tests := []struct {
		name    string
		query   string
		pattern string
	}{
		{name: "alphanumeric unchanged", query: `Events | search "powershell"`, pattern: `(^|[^[:alnum:]])powershell([^[:alnum:]]|$)`},
		{name: "IP literal", query: `Events | search "10.10.1.9" | take 100`, pattern: `(^|[^[:alnum:]])10\.10\.1\.9([^[:alnum:]]|$)`},
		{name: "regex metacharacters", query: `Events | search "a+b?(c)"`, pattern: `(^|[^[:alnum:]])a\+b\?\(c\)([^[:alnum:]]|$)`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := compiler.Compile(test.query)
			if !result.OK() {
				t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
			}
			if !strings.Contains(result.SQL, "'"+test.pattern+"'") {
				t.Fatalf("Compile() SQL = %q, want pattern %q", result.SQL, test.pattern)
			}
		})
	}
}

func TestCompileSearchRegexLimitUsesEscapedPattern(t *testing.T) {
	catalog := testCatalog{"Events": {Name: "events", Schema: ksql.Schema{Columns: []ksql.Column{{Name: "Message", Type: ksql.TypeString}}}}}
	pattern := `(^|[^[:alnum:]])a\+b\?([^[:alnum:]]|$)`
	result := ksql.New(
		dialect.PostgreSQL(),
		ksql.WithCatalog(catalog),
		ksql.WithLimits(ksql.Limits{MaxRegexBytes: len(pattern) - 1}),
	).Compile(`Events | search "a+b?"`)
	if result.OK() || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "KQLB0353" {
		t.Fatalf("Compile() = %#v", result)
	}
}

func TestCompileTermOperatorRequiresRegex(t *testing.T) {
	result := ksql.New(dialect.SQLite()).Compile(`Events | where state has "New"`)
	if result.OK() || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "KQLL0404" {
		t.Fatalf("Compile() = %#v", result)
	}
}

func TestCompileTermOperatorRejectsNonTermLiteral(t *testing.T) {
	result := ksql.New(dialect.PostgreSQL()).Compile(`Events | where RawData has "target-string"`)
	if result.OK() || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "KQLL0404" {
		t.Fatalf("Compile() = %#v", result)
	}
}

func TestCompileAsAlias(t *testing.T) {
	result := ksql.New(dialect.PostgreSQL()).Compile(`T | as LeftSide | join kind=inner (LeftSide) on id`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := `SELECT * FROM (SELECT * FROM "T") AS "_k0" INNER JOIN (SELECT * FROM "T") AS "_k1" ON ("_k0"."id" = "_k1"."id")`
	if result.SQL != want {
		t.Fatalf("Compile() SQL = %q, want %q", result.SQL, want)
	}
}

type customDialect struct{}

func (customDialect) Name() string                        { return "custom" }
func (customDialect) QuoteIdentifier(value string) string { return `"` + value + `"` }
func (customDialect) Placeholder(index int) string        { return fmt.Sprintf("$%d", index) }
func (customDialect) Boolean(value bool) string {
	if value {
		return "TRUE"
	}
	return "FALSE"
}
func (customDialect) Regex(_, _ string, _ bool) (string, bool) { return "", false }
func (customDialect) JSONIndex(value, index string) string     { return value + "[" + index + "]" }
func (customDialect) SafeCast(_, _ string) (string, bool)      { return "", false }
func (customDialect) Series(_, _, _, _, _ string) (string, bool) {
	return "", false
}
func (customDialect) ApplyLimit(sql, limit string) string { return sql + " LIMIT " + limit }
func (customDialect) SupportsNullOrdering() bool          { return true }
func (customDialect) SupportsJoinUsing() bool             { return true }

var _ dialect.Dialect = customDialect{}

func TestOptionalDialectCapabilitiesRemainOptional(t *testing.T) {
	result := ksql.New(customDialect{}).Compile(`T | sample 1`)
	if result.OK() || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "KQLL0305" {
		t.Fatalf("Compile() = %#v", result)
	}
}
