package kql

import (
	"encoding/base64"
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
const maxSourceBytes = 32 << 10

// The index stores serialized JSON, while search also examines decoded fields.
// Escaped JSON can therefore match without containing the term in the index;
// retain those rows for the ordinary search predicate to check.
const fullTextPredicate = `("rowid" IN (SELECT "rowid" FROM "events_fts" WHERE "events_fts" MATCH ?) OR instr("raw_data", '\') > 0)`

var systemSchema = ksql.Schema{Columns: []ksql.Column{
	{Name: "TimeGenerated", Type: ksql.TypeDateTime},
	{Name: "Source", Type: ksql.TypeString},
	{Name: "RawData", Type: ksql.TypeDynamic},
}}

type CompiledQuery struct {
	SQL            string
	Args           []any
	Columns        []string
	DynamicColumns map[string]struct{}
	BooleanColumns map[string]struct{}
	// TimespanColumns hold int64 ticks that the API formats as Kusto text.
	TimespanColumns map[string]struct{}
	// Visualization is the validated render annotation, when present.
	Visualization *ksql.Visualization
}

type Field struct {
	Name string
	Type string
}

type Table struct {
	ID     int64
	IDs    []int64
	Fields []Field
}

type TableCatalog map[string]Table

func (catalog TableCatalog) Columns(name string) []Field {
	schema, ok := catalog.schema(name)
	if !ok {
		return nil
	}
	fields := make([]Field, len(schema.Columns))
	for index, column := range schema.Columns {
		fields[index] = Field{Name: column.Name, Type: string(column.Type)}
	}
	return fields
}

func (catalog TableCatalog) schema(name string) (ksql.Schema, bool) {
	fields := make([]Field, 0)
	if strings.EqualFold(name, "Events") {
		byName := make(map[string]int)
		ambiguous := make(map[string]struct{})
		tableNames := make([]string, 0, len(catalog))
		for tableName := range catalog {
			tableNames = append(tableNames, tableName)
		}
		sort.Slice(tableNames, func(i, j int) bool { return strings.ToLower(tableNames[i]) < strings.ToLower(tableNames[j]) })
		for _, tableName := range tableNames {
			table := catalog[tableName]
			for _, field := range table.Fields {
				key := strings.ToLower(field.Name)
				if _, exists := ambiguous[key]; exists {
					continue
				}
				if index, exists := byName[key]; exists {
					if fields[index].Name != field.Name {
						fields[index].Name = ""
						ambiguous[key] = struct{}{}
						continue
					}
					if fields[index].Type != field.Type {
						fields[index].Type = "dynamic"
					}
					continue
				}
				byName[key] = len(fields)
				fields = append(fields, field)
			}
		}
		filtered := fields[:0]
		for _, field := range fields {
			if field.Name != "" {
				filtered = append(filtered, field)
			}
		}
		fields = filtered
		sort.Slice(fields, func(i, j int) bool { return strings.ToLower(fields[i].Name) < strings.ToLower(fields[j].Name) })
	} else {
		found := false
		for tableName, table := range catalog {
			if strings.EqualFold(name, tableName) {
				fields = append(fields, table.Fields...)
				found = true
				break
			}
		}
		if !found {
			return ksql.Schema{}, false
		}
	}
	schema := systemSchema.Clone()
	for _, field := range fields {
		schema.Columns = append(schema.Columns, ksql.Column{Name: field.Name, Type: fieldScalarType(field.Type)})
	}
	return schema, true
}

func fieldScalarType(value string) ksql.ScalarType {
	switch value {
	case "bool":
		return ksql.TypeBool
	case "int":
		return ksql.TypeInt
	case "long":
		return ksql.TypeLong
	case "real":
		return ksql.TypeReal
	case "string":
		return ksql.TypeString
	case "datetime":
		return ksql.TypeDateTime
	case "dynamic", "mixed":
		return ksql.TypeDynamic
	default:
		return ksql.TypeUnknown
	}
}

type compileSettings struct {
	tables        TableCatalog
	fullTextIndex bool
	parameters    map[string]any
	bagKeys       func(sql string, args []any) ([]ksql.BagKey, error)
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
	Parameters    map[string]any
	// BagKeys runs bag_unpack key discovery SQL, enabling bag_unpack without
	// an output schema. It receives SQL rendered like compiled queries.
	BagKeys func(sql string, args []any) ([]ksql.BagKey, error)
}

func (config CompileConfig) apply(settings *compileSettings) {
	settings.tables = config.Tables
	settings.fullTextIndex = config.FullTextIndex
	settings.parameters = config.Parameters
	settings.bagKeys = config.BagKeys
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
	// The FTS optimization parses before the compiler does, so enforce the
	// source limit here as well as in the compiler configuration.
	if len(source) > maxSourceBytes {
		return CompiledQuery{}, &Error{Message: fmt.Sprintf("query exceeds MaxSourceBytes (%d)", maxSourceBytes), Line: 1, Column: 1}
	}
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
	metadata := ksql.ManagementCatalog{}
	managementCommand := false
	if tokens, _ := ksqlkql.Lex(source); len(tokens) > 0 && tokens[0].Text == "." {
		managementCommand = true
		metadata = managementCatalog(tables)
	}
	options := append(compilerOptions(now.UTC()), compatibilityOptions()...)
	if managementCommand {
		options = append(options, ksql.WithManagementCatalog(metadata))
	}
	options = append(options,
		ksql.WithQueryParameters(settings.parameters),
		ksql.WithCatalog(catalog),
		ksql.WithSource("table", tableSource(tables, fullTextTerms)),
		ksql.WithLimits(ksql.Limits{
			MaxSourceBytes:     maxSourceBytes,
			MaxOutputRows:      resultLimit,
			MaxProjectionItems: 256,
			MaxExpansionItems:  16,
			MaxRegexBytes:      512,
			MaxJoinKeys:        16,
		}),
	)
	target := dialect.SQLite(
		dialect.WithRegexFunction("kql_regex"),
		dialect.WithRegexCaseInsensitiveFlag("(?i)"),
		dialect.WithRegexCapturesFunction("kql_regex_captures"),
		dialect.WithKeyValuesFunction("kql_key_values"),
		dialect.WithDatetimeArithmeticFunctions("kql_datetime_add_ticks", "kql_datetime_diff_ticks"),
	)
	if settings.bagKeys != nil {
		options = append(options, ksql.WithBagKeysResolver(func(query sqlast.Query, args []any) ([]ksql.BagKey, error) {
			normalizeDateTimeLiterals(query)
			sqlText, err := dialect.Render(target, query)
			if err != nil {
				return nil, err
			}
			sqlText, fullTextArgs := parameterizeFullText(sqlText, fullTextTerms)
			return settings.bagKeys(sqlText, append(args, fullTextArgs...))
		}))
	}
	compiler := ksql.New(target, options...)
	result := compiler.Compile(source)
	if diagnostic, ok := firstError(result.Diagnostics); ok {
		return CompiledQuery{}, diagnosticError(diagnostic)
	}

	columns := make([]string, 0, len(result.Columns))
	dynamic := make(map[string]struct{})
	booleans := make(map[string]struct{})
	timespans := make(map[string]struct{})
	for _, column := range result.Columns {
		columns = append(columns, column.Name)
		switch column.Type {
		case ksql.TypeDynamic:
			dynamic[column.Name] = struct{}{}
		case ksql.TypeBool:
			booleans[column.Name] = struct{}{}
		case ksql.TypeTimespan:
			timespans[column.Name] = struct{}{}
		}
	}
	query := &sqlast.Select{
		From:  &sqlast.Subquery{Query: result.Query, Alias: "result"},
		Limit: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: fmt.Sprint(resultLimit)},
	}
	normalizeDateTimeLiterals(query)
	sqlText, err := dialect.Render(target, query)
	if err != nil {
		return CompiledQuery{}, err
	}
	sqlText, fullTextArgs := parameterizeFullText(sqlText, fullTextTerms)
	return CompiledQuery{SQL: sqlText, Args: append(result.Args, fullTextArgs...), Columns: columns, DynamicColumns: dynamic, BooleanColumns: booleans, TimespanColumns: timespans, Visualization: result.Visualization}, nil
}

// KSQL emits ANSI TIMESTAMP literals, which SQLite cannot evaluate. Convert
// them in the AST so quoted SQL text is never rewritten accidentally.
func normalizeDateTimeLiterals(query sqlast.Query) {
	switch node := query.(type) {
	case *sqlast.With:
		for _, binding := range node.Bindings {
			normalizeDateTimeLiterals(binding.Query)
		}
		normalizeDateTimeLiterals(node.Query)
	case *sqlast.Select:
		for index := range node.Projections {
			node.Projections[index].Expr = normalizeDateTimeExpr(node.Projections[index].Expr)
		}
		normalizeDateTimeSource(node.From)
		node.Where = normalizeDateTimeExpr(node.Where)
		for index := range node.GroupBy {
			node.GroupBy[index] = normalizeDateTimeExpr(node.GroupBy[index])
		}
		for index := range node.OrderBy {
			node.OrderBy[index].Expr = normalizeDateTimeExpr(node.OrderBy[index].Expr)
		}
		node.Limit = normalizeDateTimeExpr(node.Limit)
	case *sqlast.Union:
		for _, child := range node.Queries {
			normalizeDateTimeLiterals(child)
		}
	}
}

func normalizeDateTimeSource(source sqlast.Source) {
	switch node := source.(type) {
	case *sqlast.Subquery:
		normalizeDateTimeLiterals(node.Query)
	case *sqlast.Join:
		normalizeDateTimeSource(node.Left)
		normalizeDateTimeSource(node.Right)
		node.On = normalizeDateTimeExpr(node.On)
	case *sqlast.Values:
		for _, row := range node.Rows {
			for index := range row {
				row[index] = normalizeDateTimeExpr(row[index])
			}
		}
	case *sqlast.Series:
		node.From = normalizeDateTimeExpr(node.From)
		node.To = normalizeDateTimeExpr(node.To)
		node.Step = normalizeDateTimeExpr(node.Step)
	case *sqlast.JSONEach:
		node.Value = normalizeDateTimeExpr(node.Value)
	}
}

func normalizeDateTimeExpr(expr sqlast.Expr) sqlast.Expr {
	switch node := expr.(type) {
	case *sqlast.Parameter:
		if node.Kind == sqlast.DateTimeLiteral {
			argument := *node
			argument.Kind = sqlast.StringLiteral
			return &sqlast.Call{Name: "kql_todatetime", Args: []sqlast.Expr{&argument}}
		}
	case *sqlast.Literal:
		if node.Kind == sqlast.StringLiteral && strings.ContainsRune(node.Value, 0) {
			// SQLite SQL text cannot contain NUL, even inside a quoted literal.
			return &sqlast.Call{Name: "kql_base64_decode_tostring", Args: []sqlast.Expr{stringLiteral(base64.StdEncoding.EncodeToString([]byte(node.Value)))}}
		}
		if node.Kind == sqlast.DateTimeLiteral {
			return &sqlast.Call{Name: "kql_todatetime", Args: []sqlast.Expr{stringLiteral(node.Value)}}
		}
	case *sqlast.Unary:
		node.Operand = normalizeDateTimeExpr(node.Operand)
	case *sqlast.Binary:
		node.Left = normalizeDateTimeExpr(node.Left)
		node.Right = normalizeDateTimeExpr(node.Right)
	case *sqlast.Window:
		node.Expr = normalizeDateTimeExpr(node.Expr)
		for index := range node.PartitionBy {
			node.PartitionBy[index] = normalizeDateTimeExpr(node.PartitionBy[index])
		}
		for index := range node.OrderBy {
			node.OrderBy[index].Expr = normalizeDateTimeExpr(node.OrderBy[index].Expr)
		}
	case *sqlast.Call:
		for index := range node.Args {
			node.Args[index] = normalizeDateTimeExpr(node.Args[index])
		}
	case *sqlast.List:
		for index := range node.Items {
			node.Items[index] = normalizeDateTimeExpr(node.Items[index])
		}
	case *sqlast.Case:
		for index := range node.Branches {
			node.Branches[index].Condition = normalizeDateTimeExpr(node.Branches[index].Condition)
			node.Branches[index].Result = normalizeDateTimeExpr(node.Branches[index].Result)
		}
		node.Else = normalizeDateTimeExpr(node.Else)
	case *sqlast.Cast:
		node.Expr = normalizeDateTimeExpr(node.Expr)
	case *sqlast.DynamicValue:
		node.Value = normalizeDateTimeExpr(node.Value)
	case *sqlast.Index:
		node.Value = normalizeDateTimeExpr(node.Value)
		node.Index = normalizeDateTimeExpr(node.Index)
	case *sqlast.Regex:
		node.Value = normalizeDateTimeExpr(node.Value)
		node.Pattern = normalizeDateTimeExpr(node.Pattern)
	case *sqlast.RegexCaptures:
		node.Value = normalizeDateTimeExpr(node.Value)
		node.Pattern = normalizeDateTimeExpr(node.Pattern)
	case *sqlast.KeyValues:
		node.Value = normalizeDateTimeExpr(node.Value)
		node.Options = normalizeDateTimeExpr(node.Options)
	case *sqlast.StringMatch:
		node.Value = normalizeDateTimeExpr(node.Value)
		node.Pattern = normalizeDateTimeExpr(node.Pattern)
	case *sqlast.ExpansionArray:
		node.Value = normalizeDateTimeExpr(node.Value)
	case *sqlast.ScalarSubquery:
		normalizeDateTimeLiterals(node.Query)
	case *sqlast.OrderedAggregate:
		for index := range node.Args {
			node.Args[index] = normalizeDateTimeExpr(node.Args[index])
		}
		for index := range node.OrderBy {
			node.OrderBy[index].Expr = normalizeDateTimeExpr(node.OrderBy[index].Expr)
		}
	case *sqlast.InSubquery:
		node.Value = normalizeDateTimeExpr(node.Value)
		normalizeDateTimeLiterals(node.Query)
	case *sqlast.ArrayContains:
		node.Array = normalizeDateTimeExpr(node.Array)
		node.Value = normalizeDateTimeExpr(node.Value)
	case *sqlast.DatetimeAdd:
		node.Datetime = normalizeDateTimeExpr(node.Datetime)
		node.Ticks = normalizeDateTimeExpr(node.Ticks)
	case *sqlast.DatetimeDiff:
		node.Left = normalizeDateTimeExpr(node.Left)
		node.Right = normalizeDateTimeExpr(node.Right)
	case *sqlast.TimespanText:
		node.Value = normalizeDateTimeExpr(node.Value)
	case *sqlast.Exists:
		normalizeDateTimeLiterals(node.Query)
	case *sqlast.RankedValue:
		node.Value = normalizeDateTimeExpr(node.Value)
	case *sqlast.Filtered:
		node.Expr = normalizeDateTimeExpr(node.Expr)
		node.Where = normalizeDateTimeExpr(node.Where)
	}
	return expr
}

func newCatalog(tables TableCatalog) ksql.Catalog {
	return ksql.CatalogFunc(func(name string) (ksql.Table, bool) {
		schema, ok := tables.schema(name)
		if ok {
			return ksql.Table{Name: "events", Schema: schema}, true
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
			{name: "raw_data", alias: "RawData"},
		}
		projections := make([]sqlast.SelectItem, 0, len(table.Schema.Columns))
		for _, column := range physical {
			projections = append(projections, sqlast.SelectItem{Expr: &sqlast.Identifier{Parts: []string{column.name}}, Alias: column.alias})
		}
		for _, column := range table.Schema.Columns[len(systemSchema.Columns):] {
			path := stringLiteral(`$."` + column.Name + `"`)
			expression := sqlast.Expr(&sqlast.Call{Name: "json_extract", Args: []sqlast.Expr{
				&sqlast.Identifier{Parts: []string{"raw_data"}}, path,
			}})
			if column.Type == ksql.TypeDynamic {
				expression = &sqlast.Binary{
					Left:     &sqlast.Identifier{Parts: []string{"raw_data"}},
					Operator: "->",
					Right:    path,
				}
			}
			projections = append(projections, sqlast.SelectItem{
				Expr:  expression,
				Alias: column.Name,
			})
		}
		query := &sqlast.Select{
			From:        &sqlast.Table{Parts: []string{"main", table.Name}},
			Projections: projections,
		}
		if !strings.EqualFold(name, "Events") {
			datasetIDs, found := datasetIDs(tables, name)
			if !found {
				return ksql.Relation{}, fmt.Errorf("unknown table %q", name)
			}
			for _, datasetID := range datasetIDs {
				predicate := sqlast.Expr(&sqlast.Binary{
					Left:     &sqlast.Identifier{Parts: []string{"dataset_id"}},
					Operator: "=",
					Right:    context.Bind(datasetID),
				})
				if query.Where == nil {
					query.Where = predicate
				} else {
					query.Where = &sqlast.Binary{Left: query.Where, Operator: "OR", Right: predicate}
				}
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
		term, err := ksqlkql.StringValue(literal)
		if err != nil {
			continue
		}
		if !utf8.ValidString(term) || utf8.RuneCountInString(term) < 3 || strings.ContainsRune(term, '\x00') {
			continue
		}
		terms[pipeline.Source.Span.Offset] = `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
	}
	return terms
}

func datasetIDs(tables TableCatalog, name string) ([]int64, bool) {
	for table, definition := range tables {
		if strings.EqualFold(name, table) {
			if len(definition.IDs) > 0 {
				return definition.IDs, true
			}
			return []int64{definition.ID}, true
		}
	}
	return nil, false
}

func compilerOptions(now time.Time) []ksql.Option {
	return []ksql.Option{
		ksql.WithTypedFunction("now", func(arguments []ksql.TypedExpression) (ksql.TypedExpression, error) {
			switch {
			case len(arguments) == 0:
				return ksql.TypedExpression{Type: ksql.TypeDateTime, SQL: stringLiteral(eventtime.Format(now))}, nil
			case len(arguments) == 1 && arguments[0].Type == ksql.TypeTimespan:
				// now(offset) is the current time shifted by a timespan.
				if ticks, ok := constantTicks(arguments[0].SQL); ok {
					return ksql.TypedExpression{Type: ksql.TypeDateTime, SQL: stringLiteral(eventtime.Format(now.Add(time.Duration(ticks) * 100)))}, nil
				}
				return ksql.TypedExpression{Type: ksql.TypeDateTime, SQL: &sqlast.Call{Name: "kql_datetime_add_ticks", Args: []sqlast.Expr{stringLiteral(eventtime.Format(now)), arguments[0].SQL}}}, nil
			}
			return ksql.TypedExpression{}, fmt.Errorf("now takes no arguments or one timespan offset")
		}),
		ksql.WithTypedFunction("ago", func(arguments []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(arguments) != 1 || (arguments[0].Type != ksql.TypeTimespan && arguments[0].Type != ksql.TypeUnknown) {
				return ksql.TypedExpression{}, fmt.Errorf("ago requires one timespan argument")
			}
			// Constant spans fold to a literal so indexed time filters stay simple.
			if ticks, ok := constantTicks(arguments[0].SQL); ok {
				return ksql.TypedExpression{Type: ksql.TypeDateTime, SQL: stringLiteral(eventtime.Format(now.Add(-time.Duration(ticks) * 100)))}, nil
			}
			negative := &sqlast.Unary{Operator: "-", Operand: arguments[0].SQL}
			return ksql.TypedExpression{Type: ksql.TypeDateTime, SQL: &sqlast.Call{Name: "kql_datetime_add_ticks", Args: []sqlast.Expr{stringLiteral(eventtime.Format(now)), negative}}}, nil
		}),
		ksql.WithFunction("datetime", datetimeFunction),
		ksql.WithTypedFunction("toint", typedRule(conversionFunction("kql_toint"), ksql.TypeInt)),
		ksql.WithTypedFunction("tolong", typedRule(conversionFunction("kql_tolong"), ksql.TypeLong)),
		ksql.WithTypedFunction("toreal", typedRule(conversionFunction("kql_toreal"), ksql.TypeReal)),
		ksql.WithTypedFunction("todouble", typedRule(conversionFunction("kql_toreal"), ksql.TypeReal)),
		ksql.WithTypedFunction("tobool", typedRule(conversionFunction("kql_tobool"), ksql.TypeBool)),
		ksql.WithTypedFunction("toboolean", typedRule(conversionFunction("kql_tobool"), ksql.TypeBool)),
		ksql.WithTypedFunction("todatetime", typedRule(ksql.SQLFunction("kql_todatetime"), ksql.TypeDateTime)),
		ksql.WithTypedFunction("toguid", typedRule(conversionFunction("kql_toguid"), ksql.TypeString)),
		ksql.WithTypedFunction("totimespan", toTimespanFunction),
		ksql.WithTypedFunction("format_datetime", func(arguments []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(arguments) != 2 || (arguments[0].Type != ksql.TypeDateTime && arguments[0].Type != ksql.TypeUnknown) || (arguments[1].Type != ksql.TypeString && arguments[1].Type != ksql.TypeUnknown) {
				return ksql.TypedExpression{}, fmt.Errorf("format_datetime requires a datetime and a format string")
			}
			return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_format_datetime", Args: []sqlast.Expr{arguments[0].SQL, arguments[1].SQL}}}, nil
		}),
		ksql.WithTypedFunction("format_timespan", func(arguments []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(arguments) != 2 || (arguments[0].Type != ksql.TypeTimespan && arguments[0].Type != ksql.TypeUnknown) || (arguments[1].Type != ksql.TypeString && arguments[1].Type != ksql.TypeUnknown) {
				return ksql.TypedExpression{}, fmt.Errorf("format_timespan requires a timespan and a format string")
			}
			return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_format_timespan", Args: []sqlast.Expr{arguments[0].SQL, arguments[1].SQL}}}, nil
		}),
		ksql.WithTypedFunction("make_timespan", func(arguments []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(arguments) < 2 || len(arguments) > 4 {
				return ksql.TypedExpression{}, fmt.Errorf("make_timespan requires 2 to 4 numeric arguments")
			}
			values := make([]sqlast.Expr, len(arguments))
			for i, argument := range arguments {
				if argument.Type != ksql.TypeInt && argument.Type != ksql.TypeLong && argument.Type != ksql.TypeReal && argument.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("make_timespan requires numeric arguments")
				}
				values[i] = argument.SQL
			}
			return ksql.TypedExpression{Type: ksql.TypeTimespan, SQL: &sqlast.Call{Name: "kql_make_timespan", Args: values}}, nil
		}),
		ksql.WithTypedFunction("totime", toTimespanFunction),
		ksql.WithTypedFunction("dayofweek", func(arguments []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(arguments) != 1 || (arguments[0].Type != ksql.TypeDateTime && arguments[0].Type != ksql.TypeUnknown) {
				return ksql.TypedExpression{}, fmt.Errorf("dayofweek requires one datetime argument")
			}
			return ksql.TypedExpression{Type: ksql.TypeTimespan, SQL: &sqlast.Call{Name: "kql_dayofweek", Args: []sqlast.Expr{arguments[0].SQL}}}, nil
		}),
		ksql.WithTypedFunction("array_length", typedRule(ksql.SQLFunction("kql_array_length"), ksql.TypeLong)),
		ksql.WithTypedFunction("bag_keys", typedRule(ksql.SQLFunction("kql_bag_keys"), ksql.TypeDynamic)),
		ksql.WithTypedFunction("bag_has_key", typedRule(ksql.SQLFunction("kql_bag_has_key"), ksql.TypeBool)),
		ksql.WithTypedFunction("base64_decode_tostring", typedRule(ksql.SQLFunction("kql_base64_decode_tostring"), ksql.TypeString)),
		ksql.WithTypedFunction("url_decode", typedRule(ksql.SQLFunction("kql_url_decode"), ksql.TypeString)),
		ksql.WithTypedFunction("ipv4_is_private", typedRule(ksql.SQLFunction("kql_ipv4_is_private"), ksql.TypeBool)),
		ksql.WithTypedFunction("ipv4_is_in_range", typedRule(ksql.SQLFunction("kql_ipv4_is_in_range"), ksql.TypeBool)),
		ksql.WithTypedFunction("extract", extractFunction),
		ksql.WithTypedFunction("isnull", typedRule(nullFunction(false), ksql.TypeBool)),
		ksql.WithTypedFunction("isnotnull", typedRule(nullFunction(true), ksql.TypeBool)),
		ksql.WithTypedFunction("isempty", typedRule(emptyFunction(false), ksql.TypeBool)),
		ksql.WithTypedFunction("isnotempty", typedRule(emptyFunction(true), ksql.TypeBool)),
		ksql.WithTypedFunction("notnull", typedRule(nullFunction(true), ksql.TypeBool)),
		ksql.WithTypedFunction("notempty", typedRule(emptyFunction(true), ksql.TypeBool)),
		ksql.WithAggregate("make_set", ksql.SQLFunction("kql_make_set_value")),
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

func conversionFunction(target string) ksql.FunctionRule {
	return func(arguments []sqlast.Expr) (sqlast.Expr, error) {
		if len(arguments) != 1 {
			return nil, fmt.Errorf("cast requires one argument")
		}
		return &sqlast.Call{Name: target, Args: arguments}, nil
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

func toTimespanFunction(arguments []ksql.TypedExpression) (ksql.TypedExpression, error) {
	if len(arguments) != 1 {
		return ksql.TypedExpression{}, fmt.Errorf("totimespan requires one argument")
	}
	if arguments[0].Type == ksql.TypeTimespan {
		return arguments[0], nil
	}
	return ksql.TypedExpression{Type: ksql.TypeTimespan, SQL: &sqlast.Call{Name: "kql_totimespan", Args: []sqlast.Expr{arguments[0].SQL}}}, nil
}

// constantTicks reads a compiled constant timespan, including negation.
func constantTicks(expr sqlast.Expr) (int64, bool) {
	sign := int64(1)
	if unary, ok := expr.(*sqlast.Unary); ok && unary.Operator == "-" {
		sign, expr = -1, unary.Operand
	}
	literal, ok := expr.(*sqlast.Literal)
	if !ok || literal.Kind != sqlast.NumberLiteral {
		return 0, false
	}
	ticks, err := strconv.ParseInt(literal.Value, 10, 64)
	return sign * ticks, err == nil
}

// extractFunction maps extract(regex, group, source [, typeof(T)]); the
// optional type converts the capture, and failed conversions are null.
func extractFunction(arguments []ksql.TypedExpression) (ksql.TypedExpression, error) {
	if len(arguments) != 3 && len(arguments) != 4 {
		return ksql.TypedExpression{}, fmt.Errorf("extract requires a regex, capture group, source and optional typeof(T)")
	}
	values := []sqlast.Expr{arguments[0].SQL, arguments[1].SQL, arguments[2].SQL}
	result := ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_extract", Args: values}}
	if len(arguments) == 3 {
		return result, nil
	}
	literal, ok := arguments[3].SQL.(*sqlast.Literal)
	if !ok || literal.Kind != sqlast.StringLiteral {
		return ksql.TypedExpression{}, fmt.Errorf("extract type must be typeof(T)")
	}
	conversions := map[string]struct {
		function string
		kind     ksql.ScalarType
	}{
		"int": {"kql_toint", ksql.TypeInt}, "int32": {"kql_toint", ksql.TypeInt},
		"long": {"kql_tolong", ksql.TypeLong}, "int64": {"kql_tolong", ksql.TypeLong},
		"real": {"kql_toreal", ksql.TypeReal}, "double": {"kql_toreal", ksql.TypeReal},
		"bool": {"kql_tobool", ksql.TypeBool}, "boolean": {"kql_tobool", ksql.TypeBool},
		"datetime": {"kql_todatetime", ksql.TypeDateTime}, "date": {"kql_todatetime", ksql.TypeDateTime},
		"timespan": {"kql_totimespan", ksql.TypeTimespan}, "time": {"kql_totimespan", ksql.TypeTimespan},
		"guid": {"kql_toguid", ksql.TypeString},
	}
	if literal.Value == "string" {
		return result, nil
	}
	conversion, ok := conversions[literal.Value]
	if !ok {
		return ksql.TypedExpression{}, fmt.Errorf("extract does not support typeof(%s)", literal.Value)
	}
	return ksql.TypedExpression{Type: conversion.kind, SQL: &sqlast.Call{Name: conversion.function, Args: []sqlast.Expr{result.SQL}}}, nil
}

// typedRule gives an untyped SQL rule its documented result type, so API
// metadata formats Booleans, dynamic values and datetimes correctly.
func typedRule(rule ksql.FunctionRule, kind ksql.ScalarType) ksql.TypedFunctionRule {
	return func(arguments []ksql.TypedExpression) (ksql.TypedExpression, error) {
		values := make([]sqlast.Expr, len(arguments))
		for i, argument := range arguments {
			values[i] = argument.SQL
		}
		expression, err := rule(values)
		if err != nil {
			return ksql.TypedExpression{}, err
		}
		return ksql.TypedExpression{Type: kind, SQL: expression}, nil
	}
}
