package kql

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	ksqlkql "github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
	"github.com/kawijayaa/striem/internal/eventtime"
)

const resultLimit = 1000
const fullTextPredicate = `"rowid" IN (SELECT "rowid" FROM "events_fts" WHERE "events_fts" MATCH ?)`

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

type compileSettings struct {
	tables        TableCatalog
	fullTextIndex bool
}

type CompileOption interface {
	apply(*compileSettings)
}

func (catalog TableCatalog) apply(settings *compileSettings) {
	settings.tables = catalog
}

type CompileConfig struct {
	Tables        TableCatalog
	FullTextIndex bool
}

func (config CompileConfig) apply(settings *compileSettings) {
	settings.tables = config.Tables
	settings.fullTextIndex = config.FullTextIndex
}

type Error struct {
	Message string `json:"message"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message)
}

func Compile(source string, now time.Time, compileOptions ...CompileOption) (CompiledQuery, error) {
	var settings compileSettings
	for _, option := range compileOptions {
		if option != nil {
			option.apply(&settings)
		}
	}
	tables := settings.tables
	fullTextTerms := map[int]string(nil)
	if settings.fullTextIndex {
		fullTextTerms = leadingSearchTerms(source)
	}
	catalog := newCatalog(tables)
	options := compilerOptions(now.UTC())
	options = append(options,
		ksql.WithCatalog(catalog),
		ksql.WithSource("table", tableSource(tables, fullTextTerms)),
		ksql.WithLimits(ksql.Limits{
			MaxSourceBytes:     32 << 10,
			MaxOutputRows:      resultLimit,
			MaxProjectionItems: 32,
			MaxExpansionItems:  1,
			MaxRegexBytes:      512,
			MaxJoinKeys:        16,
		}),
	)
	target := dialect.SQLite(
		dialect.WithRegexFunction("kql_regex"),
		dialect.WithRegexCaseInsensitiveFlag("(?i)"),
	)
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
	sqlText, fullTextArgs := parameterizeFullText(sqlText, fullTextTerms)
	return CompiledQuery{SQL: sqlText, Args: append(result.Args, fullTextArgs...), Columns: columns, DynamicColumns: dynamic}, nil
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

func tableSource(tables TableCatalog, fullTextTerms map[int]string) ksql.SourceRule {
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
		if _, found := fullTextTerms[source.Span.Offset]; found {
			predicate := sqlast.Expr(&sqlast.Call{
				Name: "striem_fts_prefilter",
				Args: []sqlast.Expr{
					&sqlast.Identifier{Parts: []string{"rowid"}},
					stringLiteral(fullTextMarker(source.Span.Offset)),
				},
			})
			if query.Where == nil {
				query.Where = predicate
			} else {
				query.Where = &sqlast.Binary{Left: query.Where, Operator: "AND", Right: predicate}
			}
		}
		return ksql.Relation{Query: query, Schema: table.Schema.Clone()}, nil
	}
}

func parameterizeFullText(sqlText string, terms map[int]string) (string, []any) {
	type replacement struct {
		start int
		end   int
		term  string
	}
	var replacements []replacement
	for offset, term := range terms {
		call := `STRIEM_FTS_PREFILTER("rowid", '` + fullTextMarker(offset) + `')`
		for position := 0; position < len(sqlText); {
			index := strings.Index(sqlText[position:], call)
			if index < 0 {
				break
			}
			index += position
			replacements = append(replacements, replacement{start: index, end: index + len(call), term: term})
			position = index + len(call)
		}
	}
	if len(replacements) == 0 {
		return sqlText, nil
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start < replacements[j].start })
	var rendered strings.Builder
	arguments := make([]any, 0, len(replacements))
	position := 0
	for _, replacement := range replacements {
		rendered.WriteString(sqlText[position:replacement.start])
		rendered.WriteString(fullTextPredicate)
		arguments = append(arguments, replacement.term)
		position = replacement.end
	}
	rendered.WriteString(sqlText[position:])
	return rendered.String(), arguments
}

func fullTextMarker(offset int) string {
	return "__striem_fts_" + strconv.Itoa(offset) + "__"
}

func leadingSearchTerms(source string) map[int]string {
	script, parseErrors := ksqlkql.Parse(source)
	if len(parseErrors) != 0 {
		return nil
	}
	terms := make(map[int]string)
	for _, statement := range script.Statements {
		var pipeline *ksqlkql.Pipeline
		switch value := statement.(type) {
		case *ksqlkql.ExpressionStatement:
			pipeline = value.Pipeline
		case *ksqlkql.LetStatement:
			pipeline = value.Pipeline
		}
		if pipeline == nil || pipeline.Source.Kind != "table" || len(pipeline.Operators) == 0 || pipeline.Operators[0].Kind != "search" {
			continue
		}
		spec, ok := pipeline.Operators[0].Body.(ksqlkql.SearchSpec)
		if !ok {
			continue
		}
		literal, ok := spec.Term.(*ksqlkql.LiteralExpression)
		if !ok || literal.Kind != ksqlkql.StringToken {
			continue
		}
		term := unquoteKQLLiteral(literal.Text)
		if !utf8.ValidString(term) || utf8.RuneCountInString(term) < 3 || strings.ContainsRune(term, '\x00') {
			continue
		}
		terms[pipeline.Source.Span.Offset] = `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
	}
	return terms
}

func unquoteKQLLiteral(value string) string {
	if len(value) > 1 && (value[0] == 'h' || value[0] == 'H') && (value[1] == '\'' || value[1] == '"' || value[1] == '@') {
		value = value[1:]
	}
	verbatim := strings.HasPrefix(value, "@")
	if verbatim {
		value = value[1:]
	}
	if len(value) >= 6 && ((strings.HasPrefix(value, "```") && strings.HasSuffix(value, "```")) || (strings.HasPrefix(value, "~~~") && strings.HasSuffix(value, "~~~"))) {
		return value[3 : len(value)-3]
	}
	if len(value) < 2 {
		return value
	}
	quote := value[0]
	if (quote != '\'' && quote != '"') || value[len(value)-1] != quote {
		return value
	}
	value = value[1 : len(value)-1]
	if verbatim {
		return strings.ReplaceAll(value, string([]byte{quote, quote}), string(quote))
	}
	if unquoted, err := strconv.Unquote(string(quote) + value + string(quote)); err == nil {
		return unquoted
	}
	return strings.ReplaceAll(value, "\\"+string(quote), string(quote))
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
	original := value
	scale := time.Duration(1)
	if strings.HasSuffix(value, "d") {
		value = strings.TrimSuffix(value, "d") + "h"
		scale = 24
	} else if strings.HasSuffix(value, "w") {
		value = strings.TrimSuffix(value, "w") + "h"
		scale = 168
	}
	duration, err := time.ParseDuration(value)
	if err != nil || scale > 1 && (duration > time.Duration(1<<63-1)/scale || duration < time.Duration(-1<<63)/scale) {
		return 0, fmt.Errorf("invalid duration %q", original)
	}
	return duration * scale, nil
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
