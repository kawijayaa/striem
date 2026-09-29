package ksql_test

import (
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

type testCatalog map[string]ksql.Table

func (c testCatalog) ResolveTable(name string) (ksql.Table, bool) {
	for logical, table := range c {
		if strings.EqualFold(logical, name) {
			return table, true
		}
	}
	return ksql.Table{}, false
}

func investigationCatalog() testCatalog {
	return testCatalog{
		"Events": {
			Name: "events",
			Schema: ksql.Schema{Columns: []ksql.Column{
				{Name: "TimeGenerated", Type: ksql.TypeDateTime},
				{Name: "Host", Type: ksql.TypeString},
				{Name: "Source", Type: ksql.TypeString},
				{Name: "EventType", Type: ksql.TypeString},
				{Name: "RawData", Type: ksql.TypeDynamic},
			}},
		},
		"UAL": {Name: "ual", Schema: ksql.Schema{Columns: []ksql.Column{
			{Name: "User", Type: ksql.TypeString}, {Name: "Host", Type: ksql.TypeString},
		}}},
		"Sysmon": {Name: "sysmon", Schema: ksql.Schema{Columns: []ksql.Column{
			{Name: "User", Type: ksql.TypeString}, {Name: "Host", Type: ksql.TypeString}, {Name: "Message", Type: ksql.TypeString},
		}}},
	}
}

func TestSchemaAwareInvestigationPipeline(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()))
	result := compiler.Compile(`Events
| extend Command=tostring(RawData.process.command_line)
| where Command contains "powershell"
| project TimeGenerated, Host, Command
| order by TimeGenerated desc
| take 100`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	wantColumns := []ksql.Column{
		{Name: "TimeGenerated", Type: ksql.TypeDateTime},
		{Name: "Host", Type: ksql.TypeString},
		{Name: "Command", Type: ksql.TypeString},
	}
	if !reflect.DeepEqual(result.Columns, wantColumns) {
		t.Fatalf("Columns = %#v, want %#v", result.Columns, wantColumns)
	}
	if !strings.Contains(result.SQL, `json_extract("RawData", '$."process"."command_line"')`) {
		t.Fatalf("SQL does not contain bound dynamic access: %s", result.SQL)
	}
}

func TestExtendReplacesColumnInPlace(t *testing.T) {
	result := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog())).Compile(`Events | extend Host="replacement" | project Host`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	if want := []ksql.Column{{Name: "Host", Type: ksql.TypeString}}; !reflect.DeepEqual(result.Columns, want) {
		t.Fatalf("Columns = %#v, want %#v", result.Columns, want)
	}
	if strings.Count(result.SQL, `AS "Host"`) != 1 {
		t.Fatalf("replacement SQL has duplicate Host outputs: %s", result.SQL)
	}
}

func TestSchemaProjectionOperators(t *testing.T) {
	result := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog())).Compile(`Events
| project-away Source, EventType
| project-rename Computer=Host
| project Computer, RawData`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := []ksql.Column{{Name: "Computer", Type: ksql.TypeString}, {Name: "RawData", Type: ksql.TypeDynamic}}
	if !reflect.DeepEqual(result.Columns, want) {
		t.Fatalf("Columns = %#v, want %#v", result.Columns, want)
	}
}

func TestJoinSchemaCollisionSuffixes(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()))
	result := compiler.Compile(`UAL | project User, Host | join kind=inner (Sysmon | project User, Host, Message) on User`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := []ksql.Column{
		{Name: "User", Type: ksql.TypeString}, {Name: "Host", Type: ksql.TypeString},
		{Name: "User1", Type: ksql.TypeString}, {Name: "Host1", Type: ksql.TypeString},
		{Name: "Message", Type: ksql.TypeString},
	}
	if !reflect.DeepEqual(result.Columns, want) {
		t.Fatalf("Columns = %#v, want %#v", result.Columns, want)
	}
	if !strings.Contains(result.SQL, `AS "User1"`) || !strings.Contains(result.SQL, `AS "Host1"`) {
		t.Fatalf("SQL lacks explicit collision aliases: %s", result.SQL)
	}
}

func TestLeftAntiJoinSchema(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()))
	result := compiler.Compile(`UAL | project User, Host | join kind=leftanti (Sysmon | project User) on User`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	if !strings.Contains(result.SQL, "NOT EXISTS") {
		t.Fatalf("SQL = %s", result.SQL)
	}
	want := []ksql.Column{{Name: "User", Type: ksql.TypeString}, {Name: "Host", Type: ksql.TypeString}}
	if !reflect.DeepEqual(result.Columns, want) {
		t.Fatalf("Columns = %#v, want %#v", result.Columns, want)
	}
}

func TestBoundDiagnostics(t *testing.T) {
	tests := []struct {
		name  string
		query string
		code  string
	}{
		{name: "table", query: `Missing | take 1`, code: "KQLB0201"},
		{name: "column", query: `Events | project Missing`, code: "KQLB0401"},
		{name: "duplicate", query: `Events | project Host, host`, code: "KQLB0406"},
		{name: "dynamic", query: `Events | project Host.name`, code: "KQLB0403"},
		{name: "join key", query: `UAL | join kind=inner (Sysmon) on Missing`, code: "KQLB0310"},
	}
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()))
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := compiler.Compile(test.query)
			if result.OK() || result.SQL != "" {
				t.Fatalf("Compile() = %#v", result)
			}
			found := false
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == test.code && diagnostic.Phase == ksql.PhaseBind && diagnostic.Span.Length > 0 {
					found = true
				}
			}
			if !found {
				t.Fatalf("diagnostics = %#v, want positioned %s", result.Diagnostics, test.code)
			}
		})
	}
}

func TestAmbiguousCatalogColumnDiagnostic(t *testing.T) {
	catalog := testCatalog{"T": {Name: "t", Schema: ksql.Schema{Columns: []ksql.Column{
		{Name: "Host", Type: ksql.TypeString}, {Name: "host", Type: ksql.TypeString},
	}}}}
	result := ksql.New(dialect.SQLite(), ksql.WithCatalog(catalog)).Compile(`T | project HOST`)
	if result.OK() || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "KQLB0402" {
		t.Fatalf("Compile() = %#v", result)
	}
}

func TestParameterOrderingAcrossNestedRelations(t *testing.T) {
	compiler := ksql.New(dialect.PostgreSQL(), ksql.WithCatalog(investigationCatalog()), ksql.WithParameters())
	result := compiler.Compile(`Events | where Host == "left" | union (Events | where Host == "union") | join kind=inner (Sysmon | where Message == "right") on Host`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	want := []any{"left", "union", "right"}
	if !reflect.DeepEqual(result.Args, want) {
		t.Fatalf("Args = %#v, want %#v", result.Args, want)
	}
	for _, placeholder := range []string{"$1", "$2", "$3"} {
		if !strings.Contains(result.SQL, placeholder) {
			t.Fatalf("SQL lacks %s: %s", placeholder, result.SQL)
		}
	}
}

func TestQualifiedDynamicJoinAccess(t *testing.T) {
	result := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog())).Compile(`Events | join kind=inner (Events) on $left.RawData.process.command_line == $right.RawData.process.command_line`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	if !strings.Contains(result.SQL, `json_extract("_k0"."RawData", '$."process"."command_line"')`) || !strings.Contains(result.SQL, `json_extract("_k1"."RawData", '$."process"."command_line"')`) {
		t.Fatalf("qualified dynamic SQL = %s", result.SQL)
	}
}

func TestParameterizedDynamicIndexRetainsJSONEach(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()), ksql.WithParameters())
	result := compiler.Compile(`Events | project Nested=RawData.process.command_line, Bound=RawData["a.b"]`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	if !strings.Contains(result.SQL, `json_extract("RawData", '$."process"."command_line"')`) {
		t.Fatalf("SQL does not collapse literal dynamic path: %s", result.SQL)
	}
	if !strings.Contains(result.SQL, `(SELECT value FROM json_each("RawData") WHERE key = ? LIMIT 1)`) {
		t.Fatalf("SQL does not retain parameterized json_each access: %s", result.SQL)
	}
	if want := []any{"a.b"}; !reflect.DeepEqual(result.Args, want) {
		t.Fatalf("Args = %#v, want %#v", result.Args, want)
	}
}

func TestParameterizedLiteralsAndOrdering(t *testing.T) {
	compiler := ksql.New(dialect.PostgreSQL(), ksql.WithCatalog(investigationCatalog()), ksql.WithParameters())
	result := compiler.Compile(`Events | where Host == "x' OR 1=1 --" and Source == "second" | take 7`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	wantArgs := []any{"x' OR 1=1 --", "second", int64(7)}
	if !reflect.DeepEqual(result.Args, wantArgs) {
		t.Fatalf("Args = %#v, want %#v", result.Args, wantArgs)
	}
	if strings.Contains(result.SQL, "OR 1=1") || !strings.Contains(result.SQL, "$1") || !strings.Contains(result.SQL, "$3") {
		t.Fatalf("parameterized SQL = %s", result.SQL)
	}
}

func TestSourceRuleRunsAfterTabularBinding(t *testing.T) {
	var calls atomic.Int32
	catalog := investigationCatalog()
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(catalog), ksql.WithParameters(), ksql.WithSource("table", func(context ksql.LoweringContext, source kql.Source) (ksql.Relation, error) {
		calls.Add(1)
		table, ok := context.Catalog().ResolveTable(source.Name)
		if !ok {
			return ksql.Relation{}, nil
		}
		return ksql.Relation{
			Query: &sqlast.Select{
				From:  &sqlast.Table{Parts: []string{table.Name}},
				Where: &sqlast.Binary{Left: &sqlast.Identifier{Parts: []string{"dataset_id"}}, Operator: "=", Right: context.Bind(source.Name)},
			},
			Schema: table.Schema,
		}, nil
	}))
	result := compiler.Compile(`let E = Events | project Host; E | take 1`)
	if !result.OK() {
		t.Fatalf("Compile() diagnostics = %#v", result.Diagnostics)
	}
	if calls.Load() != 1 {
		t.Fatalf("source rule calls = %d, want 1", calls.Load())
	}
	if !reflect.DeepEqual(result.Args, []any{"Events", int64(1)}) {
		t.Fatalf("Args = %#v", result.Args)
	}
}

func TestLimitsArePositionedDiagnostics(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()), ksql.WithLimits(ksql.Limits{MaxProjectionItems: 1, MaxOutputRows: 5}))
	for _, query := range []string{`Events | project Host, Source`, `Events | take 6`} {
		result := compiler.Compile(query)
		if result.OK() || result.Diagnostics[0].Span.Length == 0 {
			t.Fatalf("Compile(%q) = %#v", query, result)
		}
	}
}
