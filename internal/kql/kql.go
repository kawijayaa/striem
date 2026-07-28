package kql

import (
	"fmt"
	"strings"
	"time"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	ksqlkql "github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
	"github.com/kawijayaa/striem/internal/eventtime"
)

const resultLimit = 1000

var eventSchema = ksql.Schema{Columns: []ksql.Column{
	{Name: "TimeGenerated", Type: ksql.TypeDateTime},
	{Name: "Source", Type: ksql.TypeString},
	{Name: "EventType", Type: ksql.TypeString},
	{Name: "Host", Type: ksql.TypeString},
	{Name: "User", Type: ksql.TypeString},
	{Name: "Message", Type: ksql.TypeString},
	{Name: "RawData", Type: ksql.TypeDynamic},
}}

type CompiledQuery struct {
	SQL            string
	Args           []any
	Columns        []string
	DynamicColumns map[string]struct{}
}

type TableCatalog map[string]int64

type Error struct {
	Message string `json:"message"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message)
}

func Compile(source string, now time.Time, catalogs ...TableCatalog) (CompiledQuery, error) {
	var tables TableCatalog
	if len(catalogs) > 0 {
		tables = catalogs[0]
	}
	catalog := newCatalog(tables)
	options := compilerOptions(now.UTC())
	options = append(options,
		ksql.WithCatalog(catalog),
		ksql.WithSource("table", tableSource(tables)),
		ksql.WithLimits(ksql.Limits{
			MaxSourceBytes:     32 << 10,
			MaxOutputRows:      resultLimit,
			MaxProjectionItems: 32,
			MaxExpansionItems:  1,
			MaxRegexBytes:      512,
			MaxJoinKeys:        16,
		}),
	)
	target := dialect.SQLite(dialect.WithRegexFunction("kql_regex"))
	compiler := ksql.New(target, options...)
	result := compiler.Compile(source)
	if diagnostic, ok := firstError(result.Diagnostics); ok {
		return CompiledQuery{}, diagnosticError(diagnostic)
	}

	columns := make([]string, 0, len(result.Columns))
	dynamic := make(map[string]struct{})
	for _, column := range result.Columns {
		columns = append(columns, column.Name)
		if column.Type == ksql.TypeDynamic {
			dynamic[column.Name] = struct{}{}
		}
	}
	query := &sqlast.Select{
		From:  &sqlast.Subquery{Query: result.Query, Alias: "result"},
		Limit: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: fmt.Sprint(resultLimit)},
	}
	sqlText, err := dialect.Render(target, query)
	if err != nil {
		return CompiledQuery{}, err
	}
	return CompiledQuery{SQL: sqlText, Args: result.Args, Columns: columns, DynamicColumns: dynamic}, nil
}

func newCatalog(tables TableCatalog) ksql.Catalog {
	return ksql.CatalogFunc(func(name string) (ksql.Table, bool) {
		if strings.EqualFold(name, "Events") {
			return ksql.Table{Name: "events", Schema: eventSchema.Clone()}, true
		}
		for table := range tables {
			if strings.EqualFold(name, table) {
				return ksql.Table{Name: "events", Schema: eventSchema.Clone()}, true
			}
		}
		return ksql.Table{}, false
	})
}

func tableSource(tables TableCatalog) ksql.SourceRule {
	return func(context ksql.LoweringContext, source ksqlkql.Source) (ksql.Relation, error) {
		name := unquoteName(source.Name)
		table, ok := context.Catalog().ResolveTable(name)
		if !ok {
			return ksql.Relation{}, fmt.Errorf("unknown table %q", name)
		}
		physical := []struct {
			name  string
			alias string
		}{
			{name: "time_generated", alias: "TimeGenerated"},
			{name: "source", alias: "Source"},
			{name: "event_type", alias: "EventType"},
			{name: "host", alias: "Host"},
			{name: "username", alias: "User"},
			{name: "message", alias: "Message"},
			{name: "raw_data", alias: "RawData"},
		}
		projections := make([]sqlast.SelectItem, len(physical))
		for index, column := range physical {
			projections[index] = sqlast.SelectItem{Expr: &sqlast.Identifier{Parts: []string{column.name}}, Alias: column.alias}
		}
		query := &sqlast.Select{
			From:        &sqlast.Table{Parts: []string{"main", table.Name}},
			Projections: projections,
		}
		if !strings.EqualFold(name, "Events") {
			datasetID, found := datasetID(tables, name)
			if !found {
				return ksql.Relation{}, fmt.Errorf("unknown table %q", name)
			}
			query.Where = &sqlast.Binary{
				Left:     &sqlast.Identifier{Parts: []string{"dataset_id"}},
				Operator: "=",
				Right:    context.Bind(datasetID),
			}
		}
		return ksql.Relation{Query: query, Schema: table.Schema.Clone()}, nil
	}
}

func datasetID(tables TableCatalog, name string) (int64, bool) {
	for table, id := range tables {
		if strings.EqualFold(name, table) {
			return id, true
		}
	}
	return 0, false
}

func compilerOptions(now time.Time) []ksql.Option {
	return []ksql.Option{
		ksql.WithFunction("now", func(arguments []sqlast.Expr) (sqlast.Expr, error) {
			if len(arguments) != 0 {
				return nil, fmt.Errorf("now requires no arguments")
			}
			return stringLiteral(eventtime.Format(now)), nil
		}),
		ksql.WithFunction("ago", func(arguments []sqlast.Expr) (sqlast.Expr, error) {
			if len(arguments) != 1 {
				return nil, fmt.Errorf("ago requires one argument")
			}
			literal, ok := arguments[0].(*sqlast.Literal)
			if !ok || literal.Kind != sqlast.IntervalLiteral {
				return nil, fmt.Errorf("ago requires a constant duration")
			}
			duration, err := parseDuration(literal.Value)
			if err != nil {
				return nil, err
			}
			return stringLiteral(eventtime.Format(now.Add(-duration))), nil
		}),
		ksql.WithFunction("datetime", datetimeFunction),
		ksql.WithFunction("toint", castFunction("INTEGER")),
		ksql.WithFunction("tolong", castFunction("INTEGER")),
		ksql.WithFunction("toreal", castFunction("REAL")),
		ksql.WithFunction("todouble", castFunction("REAL")),
		ksql.WithFunction("todatetime", ksql.SQLFunction("kql_todatetime")),
		ksql.WithFunction("parse_json", ksql.SQLFunction("json")),
		ksql.WithFunction("parsejson", ksql.SQLFunction("json")),
		ksql.WithFunction("array_length", ksql.SQLFunction("kql_array_length")),
		ksql.WithFunction("bag_keys", ksql.SQLFunction("kql_bag_keys")),
		ksql.WithFunction("bag_has_key", ksql.SQLFunction("kql_bag_has_key")),
		ksql.WithFunction("set_has_element", ksql.SQLFunction("kql_set_has_element")),
		ksql.WithFunction("base64_decode_tostring", ksql.SQLFunction("kql_base64_decode_tostring")),
		ksql.WithFunction("url_decode", ksql.SQLFunction("kql_url_decode")),
		ksql.WithFunction("ipv4_is_private", ksql.SQLFunction("kql_ipv4_is_private")),
		ksql.WithFunction("ipv4_is_in_range", ksql.SQLFunction("kql_ipv4_is_in_range")),
		ksql.WithFunction("split", ksql.SQLFunction("kql_split")),
		ksql.WithFunction("extract", ksql.SQLFunction("kql_extract")),
		ksql.WithFunction("trim", ksql.SQLFunction("kql_trim")),
		ksql.WithFunction("replace_string", ksql.SQLFunction("replace")),
		ksql.WithFunction("isnull", nullFunction(false)),
		ksql.WithFunction("isnotnull", nullFunction(true)),
		ksql.WithFunction("isempty", emptyFunction(false)),
		ksql.WithFunction("isnotempty", emptyFunction(true)),
		ksql.WithFunction("make_set", ksql.SQLFunction("kql_make_set_value")),
		ksql.WithFunction("take_any", ksql.SQLFunction("min")),
	}
}

func datetimeFunction(arguments []sqlast.Expr) (sqlast.Expr, error) {
	if len(arguments) != 1 {
		return nil, fmt.Errorf("datetime requires one argument")
	}
	literal, ok := arguments[0].(*sqlast.Literal)
	if !ok || literal.Kind != sqlast.StringLiteral {
		return nil, fmt.Errorf("datetime requires a constant string")
	}
	value, err := time.Parse(time.RFC3339Nano, literal.Value)
	if err != nil {
		return nil, fmt.Errorf("invalid datetime %q", literal.Value)
	}
	return stringLiteral(eventtime.Format(value)), nil
}

func nullFunction(negative bool) ksql.FunctionRule {
	return func(arguments []sqlast.Expr) (sqlast.Expr, error) {
		if len(arguments) != 1 {
			return nil, fmt.Errorf("null check requires one argument")
		}
		operator := "IS"
		if negative {
			operator = "IS NOT"
		}
		return &sqlast.Binary{Left: arguments[0], Operator: operator, Right: &sqlast.Literal{Kind: sqlast.NullLiteral}}, nil
	}
}

func emptyFunction(negative bool) ksql.FunctionRule {
	return func(arguments []sqlast.Expr) (sqlast.Expr, error) {
		if len(arguments) != 1 {
			return nil, fmt.Errorf("empty check requires one argument")
		}
		null := &sqlast.Binary{Left: arguments[0], Operator: "IS", Right: &sqlast.Literal{Kind: sqlast.NullLiteral}}
		empty := &sqlast.Binary{Left: arguments[0], Operator: "=", Right: stringLiteral("")}
		result := sqlast.Expr(&sqlast.Binary{Left: null, Operator: "OR", Right: empty})
		if negative {
			result = &sqlast.Unary{Operator: "NOT ", Operand: result}
		}
		return result, nil
	}
}

func castFunction(target string) ksql.FunctionRule {
	return func(arguments []sqlast.Expr) (sqlast.Expr, error) {
		if len(arguments) != 1 {
			return nil, fmt.Errorf("cast requires one argument")
		}
		return &sqlast.Cast{Expr: arguments[0], Type: target}, nil
	}
}

func stringLiteral(value string) sqlast.Expr {
	return &sqlast.Literal{Kind: sqlast.StringLiteral, Value: value}
}

func parseDuration(value string) (time.Duration, error) {
	if strings.HasSuffix(value, "d") {
		value = strings.TrimSuffix(value, "d") + "24h"
	} else if strings.HasSuffix(value, "w") {
		value = strings.TrimSuffix(value, "w") + "168h"
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", value)
	}
	return duration, nil
}

func firstError(diagnostics []ksql.Diagnostic) (ksql.Diagnostic, bool) {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == ksql.SeverityError {
			return diagnostic, true
		}
	}
	return ksql.Diagnostic{}, false
}

func diagnosticError(diagnostic ksql.Diagnostic) error {
	line, column := diagnostic.Span.Line, diagnostic.Span.Column
	if line < 1 {
		line = 1
	}
	if column < 1 {
		column = 1
	}
	return &Error{Message: diagnostic.Message, Line: line, Column: column}
}

func unquoteName(value string) string {
	if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}
	return value
}
