package ksql

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/kawijayaa/ksql/dialect"
	featureledger "github.com/kawijayaa/ksql/features"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

// Compiler translates KQL into one configured SQL dialect. A Compiler is safe
// for concurrent use after construction.
type Compiler struct {
	dialect            dialect.Dialect
	functionRules      map[string]FunctionRule
	typedFunctionRules map[string]TypedFunctionRule
	operatorRules      map[string]OperatorRule
	sourceRules        map[string]SourceRule
	catalog            Catalog
	parameterization   ParameterizationMode
	limits             Limits
}

// New constructs a compiler for target. A nil target selects ANSI SQL.
func New(target dialect.Dialect, options ...Option) *Compiler {
	if target == nil {
		target = dialect.ANSI()
	}
	compiler := &Compiler{
		dialect: target, functionRules: make(map[string]FunctionRule), typedFunctionRules: make(map[string]TypedFunctionRule),
		operatorRules: make(map[string]OperatorRule), sourceRules: make(map[string]SourceRule),
	}
	for _, option := range options {
		if option != nil {
			option(compiler)
		}
	}
	return compiler
}

// Result contains either SQL or error diagnostics. SQL is empty whenever an
// error diagnostic is present.
type Result struct {
	SQL         string
	Args        []any
	Tree        *kql.Script
	Query       sqlast.Query
	Diagnostics []Diagnostic
	Features    []FeatureUse
	Columns     []Column
}

// OK reports whether compilation completed without an error.
func (r Result) OK() bool {
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Severity == SeverityError {
			return false
		}
	}
	return true
}

// Compile parses and translates source. The method never emits partial SQL.
func (c *Compiler) Compile(source string) Result {
	if c.limits.MaxSourceBytes > 0 && len(source) > c.limits.MaxSourceBytes {
		return Result{Diagnostics: []Diagnostic{{Code: "KQLB0001", Severity: SeverityError, Phase: PhaseBind, Message: fmt.Sprintf("query exceeds MaxSourceBytes (%d)", c.limits.MaxSourceBytes), Span: kql.Span{Offset: 0, Length: len(source), Line: 1, Column: 1}}}}
	}
	tree, parseErrors := kql.Parse(source)
	result := Result{Tree: tree}
	for _, parseError := range parseErrors {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code: parseError.Code, Severity: SeverityError, Phase: PhaseParse,
			Message: parseError.Message, Span: parseError.Span,
		})
	}
	if !result.OK() {
		return result
	}

	state := compileState{compiler: c, scalarLets: make(map[string]boundExpr), tabularLets: make(map[string]Relation)}
	var relation Relation
	for _, statement := range tree.Statements {
		switch node := statement.(type) {
		case *kql.LetStatement:
			if node.Pipeline != nil {
				value := state.pipeline(node.Pipeline)
				if value.Query != nil {
					state.tabularLets[strings.ToLower(node.Name)] = value
				}
			} else if node.Value != nil {
				value := state.expression(node.Value)
				if value.SQL != nil {
					state.scalarLets[strings.ToLower(node.Name)] = value
				}
			} else {
				state.error("KQLL0101", node.Span, "statement.let", "function declarations require semantic parameter binding")
			}
			state.use("statement.let", "equivalent", node.Span)
		case *kql.ControlStatement:
			state.error("KQLL0102", node.Span, "statement."+node.Kind, node.Kind+" is a KQL session/control statement with no SQL query equivalent")
		case *kql.ExpressionStatement:
			if relation.Query != nil {
				state.error("KQLL0103", node.Span, "statement.expression", "multiple result-producing statements cannot be represented by one SQL query")
				continue
			}
			relation = state.pipeline(node.Pipeline)
		}
	}
	result.Diagnostics = state.diagnostics
	result.Features = state.features
	if !result.OK() || relation.Query == nil {
		return result
	}
	state.args = sqlast.NormalizeParameters(relation.Query, state.args)
	result.Args = append([]any(nil), state.args...)

	sql, err := dialect.Render(c.dialect, relation.Query)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code: "SQLD0001", Severity: SeverityError, Phase: PhaseSQL,
			Message: err.Error(),
		})
		return result
	}
	result.SQL = sql
	result.Query = relation.Query
	if !relation.Schema.Unknown {
		result.Columns = append([]Column(nil), relation.Schema.Columns...)
	}
	return result
}

type compileState struct {
	compiler    *Compiler
	scalarLets  map[string]boundExpr
	tabularLets map[string]Relation
	diagnostics []Diagnostic
	features    []FeatureUse
	args        []any
	alias       int
	qualifiers  map[string]relationScope
	qualifier   string
	input       Schema
}

type boundExpr struct {
	SQL  sqlast.Expr
	Type ScalarType
	Name string
}

type relationScope struct {
	alias  string
	schema Schema
}

func (s *compileState) pipeline(pipeline *kql.Pipeline) Relation {
	if pipeline == nil {
		return Relation{}
	}
	relation := s.source(pipeline.Source)
	for _, operator := range pipeline.Operators {
		if relation.Query == nil {
			return Relation{}
		}
		relation = s.operator(relation, operator)
	}
	return relation
}

func (s *compileState) source(source kql.Source) Relation {
	s.use("source."+source.Kind, "equivalent", source.Span)
	if source.Kind == "table" {
		if bound, ok := s.tabularLets[strings.ToLower(source.Name)]; ok {
			return bound
		}
	}
	if rule, ok := s.compiler.sourceRules[strings.ToLower(source.Kind)]; ok {
		relation, err := rule(&loweringContext{state: s, span: source.Span}, source)
		if err != nil {
			s.error("KQLL0203", source.Span, "source."+source.Kind, err.Error())
			return Relation{}
		}
		if relation.Query == nil {
			s.error("KQLL0204", source.Span, "source."+source.Kind, "custom source rule returned a nil query")
		}
		return relation
	}
	switch source.Kind {
	case "table":
		name := source.Name
		schema := Schema{Unknown: true}
		if s.compiler.catalog != nil {
			table, ok := s.compiler.catalog.ResolveTable(source.Name)
			if !ok {
				s.bindError("KQLB0201", source.Span, "source.table", "unknown table "+source.Name)
				return Relation{}
			}
			name, schema = table.Name, table.Schema.Clone()
			if name == "" {
				name = source.Name
			}
		}
		return Relation{Query: &sqlast.Select{From: &sqlast.Table{Parts: splitName(name)}}, Schema: schema}
	case "union":
		return s.union(Relation{}, kql.Operator{Kind: "union", Body: *source.Union, Span: source.Span})
	case "print":
		items, schema := s.projectItems(source.Items, true, Schema{})
		return Relation{Query: &sqlast.Select{Projections: items}, Schema: schema}
	case "datatable":
		columns := make([]string, len(source.Columns))
		for i, column := range source.Columns {
			columns[i] = column.Name
		}
		rows := make([][]sqlast.Expr, len(source.Rows))
		for i, row := range source.Rows {
			rows[i] = s.expressions(row)
		}
		schema := Schema{Columns: make([]Column, len(source.Columns))}
		for i, column := range source.Columns {
			schema.Columns[i] = Column{Name: column.Name, Type: parseScalarType(column.Type)}
		}
		return Relation{Query: &sqlast.Select{From: &sqlast.Values{Columns: columns, Rows: rows, Alias: s.nextAlias()}}, Schema: schema}
	case "range":
		return Relation{Query: &sqlast.Select{From: &sqlast.Series{Column: source.Name, From: s.expression(source.From).SQL, To: s.expression(source.To).SQL, Step: s.expression(source.Step).SQL, Alias: s.nextAlias()}}, Schema: Schema{Columns: []Column{{Name: source.Name, Type: TypeLong}}}}
	case "expression":
		if call, ok := source.Expr.(*kql.CallExpression); ok {
			if table := s.entityCall(call); table != nil {
				return Relation{Query: &sqlast.Select{From: table}, Schema: Schema{Unknown: true}}
			}
		}
		s.error("KQLL0201", source.Span, "source.expression", "expression is not a supported tabular source")
	default:
		s.error("KQLL0202", source.Span, "source."+source.Kind, source.Kind+" has no generic SQL source equivalent")
	}
	return Relation{}
}

func (s *compileState) entityCall(call *kql.CallExpression) *sqlast.Table {
	name, ok := call.Function.(*kql.NameExpression)
	if !ok || len(call.Args) != 1 {
		return nil
	}
	function := strings.ToLower(name.Name)
	if function != "table" && function != "external_table" && function != "materialized_view" {
		return nil
	}
	literal, ok := call.Args[0].(*kql.LiteralExpression)
	if !ok || literal.Kind != kql.StringToken {
		return nil
	}
	return &sqlast.Table{Parts: []string{unquoteKQL(literal.Text)}}
}

func (s *compileState) operator(input Relation, operator kql.Operator) Relation {
	feature := "operator." + operator.Kind
	s.use(feature, "equivalent", operator.Span)
	if rule, ok := s.compiler.operatorRules[strings.ToLower(operator.Kind)]; ok {
		relation, err := rule(&loweringContext{state: s, input: input.Schema, span: operator.Span}, input, operator)
		if err != nil {
			s.error("KQLL0303", operator.Span, feature, err.Error())
			return Relation{}
		}
		if relation.Query == nil {
			s.error("KQLL0304", operator.Span, feature, "custom operator rule returned a nil query")
		}
		return relation
	}
	if operator.Kind == "join" || operator.Kind == "lookup" {
		return s.join(input, operator)
	}
	if operator.Kind == "union" {
		return s.union(input, operator)
	}
	if operator.Kind == "mv-expand" {
		return s.mvExpand(input, operator.Body.(kql.MvExpandSpec), operator)
	}
	if operator.Kind == "mv-apply" {
		return s.mvApply(input, operator.Body.(kql.MvApplySpec), operator)
	}
	if operator.Kind == "as" {
		spec := operator.Body.(kql.AsSpec)
		if spec.Materialized != nil && *spec.Materialized {
			s.error("KQLL0306", operator.Span, feature, "as hint.materialized=true requires guaranteed single evaluation")
			return Relation{}
		}
		s.tabularLets[strings.ToLower(spec.Name)] = input
		return input
	}
	from := &sqlast.Subquery{Query: input.Query, Alias: s.nextAlias()}
	previousInput := s.input
	s.input = input.Schema
	defer func() { s.input = previousInput }()
	switch operator.Kind {
	case "where":
		spec := operator.Body.(kql.ExpressionSpec)
		condition := s.expression(spec.Expressions[0])
		if condition.Type != TypeBool && condition.Type != TypeUnknown {
			s.bindError("KQLB0301", spec.Expressions[0].NodeSpan(), feature, "where expression must be bool")
		}
		return Relation{Query: &sqlast.Select{From: from, Where: condition.SQL}, Schema: input.Schema.Clone()}
	case "project":
		spec := operator.Body.(kql.ExpressionSpec)
		if !s.checkProjectionLimit(spec.Expressions, operator) {
			return Relation{}
		}
		items, schema := s.projectItems(spec.Expressions, false, input.Schema)
		return Relation{Query: &sqlast.Select{From: from, Projections: items}, Schema: schema}
	case "extend":
		spec := operator.Body.(kql.ExpressionSpec)
		return s.extend(from, input.Schema, spec.Expressions, operator)
	case "project-away", "project-keep", "project-rename", "project-reorder":
		return s.projectionOperator(from, input.Schema, operator)
	case "summarize":
		spec := operator.Body.(kql.SummarizeSpec)
		groups := s.expressions(spec.By)
		groupItems, groupSchema := s.projectItems(spec.By, true, input.Schema)
		aggregateItems, aggregateSchema := s.projectItems(spec.Aggregates, true, input.Schema)
		groupSchema.Columns = append(groupSchema.Columns, aggregateSchema.Columns...)
		s.validateUnique(groupSchema.Columns, operator.Span, feature)
		return Relation{Query: &sqlast.Select{From: from, Projections: append(groupItems, aggregateItems...), GroupBy: groups}, Schema: groupSchema}
	case "distinct":
		spec := operator.Body.(kql.ExpressionSpec)
		items, schema := s.projectItems(spec.Expressions, false, input.Schema)
		return Relation{Query: &sqlast.Select{Distinct: true, From: from, Projections: items}, Schema: schema}
	case "count":
		alias := "Count"
		if raw, ok := operator.Body.(kql.RawSpec); ok && len(raw.Tokens) == 2 && strings.EqualFold(raw.Tokens[0].Text, "as") {
			alias = raw.Tokens[1].Text
		}
		return Relation{Query: &sqlast.Select{From: from, Projections: []sqlast.SelectItem{{Expr: &sqlast.Call{Name: "count", Args: []sqlast.Expr{&sqlast.Star{}}}, Alias: alias}}}, Schema: Schema{Columns: []Column{{Name: alias, Type: TypeLong}}}}
	case "sort":
		spec := operator.Body.(kql.SortSpec)
		orders := make([]sqlast.Order, len(spec.Expressions))
		for i, order := range spec.Expressions {
			orders[i] = sqlast.Order{Expr: s.expression(order.Expression).SQL, Direction: order.Direction, Nulls: order.Nulls}
		}
		return Relation{Query: &sqlast.Select{From: from, OrderBy: orders}, Schema: input.Schema.Clone()}
	case "take":
		spec := operator.Body.(kql.ExpressionSpec)
		if !s.validateLimit(spec.Expressions[0], operator) {
			return Relation{}
		}
		return Relation{Query: &sqlast.Select{From: from, Limit: s.expression(spec.Expressions[0]).SQL}, Schema: input.Schema.Clone()}
	case "top":
		spec := operator.Body.(kql.TopSpec)
		if !s.validateLimit(spec.Count, operator) {
			return Relation{}
		}
		return Relation{Query: &sqlast.Select{From: from, OrderBy: []sqlast.Order{{Expr: s.expression(spec.By.Expression).SQL, Direction: spec.By.Direction, Nulls: spec.By.Nulls}}, Limit: s.expression(spec.Count).SQL}, Schema: input.Schema.Clone()}
	case "search":
		return s.search(from, input.Schema, operator)
	case "sample":
		if !supportsRandom(s.compiler.dialect) {
			s.error("KQLL0305", operator.Span, feature, "dialect "+s.compiler.dialect.Name()+" has no random sampling expression")
			return Relation{}
		}
		spec := operator.Body.(kql.ExpressionSpec)
		if !s.validateLimit(spec.Expressions[0], operator) {
			return Relation{}
		}
		return Relation{Query: &sqlast.Select{From: from, OrderBy: []sqlast.Order{{Expr: &sqlast.Random{}}}, Limit: s.expression(spec.Expressions[0]).SQL}, Schema: input.Schema.Clone()}
	case "sample-distinct":
		if !supportsRandom(s.compiler.dialect) {
			s.error("KQLL0305", operator.Span, feature, "dialect "+s.compiler.dialect.Name()+" has no random sampling expression")
			return Relation{}
		}
		spec := operator.Body.(kql.SampleDistinctSpec)
		if !s.validateLimit(spec.Count, operator) {
			return Relation{}
		}
		column := s.expression(spec.Column)
		inner := &sqlast.Select{Distinct: true, From: from, Projections: []sqlast.SelectItem{{Expr: column.SQL}}}
		return Relation{Query: &sqlast.Select{From: &sqlast.Subquery{Query: inner, Alias: s.nextAlias()}, OrderBy: []sqlast.Order{{Expr: &sqlast.Random{}}}, Limit: s.expression(spec.Count).SQL}, Schema: Schema{Columns: []Column{{Name: column.Name, Type: column.Type}}}}
	case "serialize":
		s.warn("KQLL0301", operator.Span, feature, "serialize metadata is unnecessary in SQL; row order still requires an explicit sort")
		return Relation{Query: &sqlast.Select{From: from}, Schema: input.Schema.Clone()}
	default:
		s.error("KQLL0302", operator.Span, feature, operator.Kind+" is recognized but has no enabled lowering for "+s.compiler.dialect.Name())
		return Relation{}
	}
}

func (s *compileState) projectItems(expressions []kql.Expression, inferAlias bool, input Schema) ([]sqlast.SelectItem, Schema) {
	items := make([]sqlast.SelectItem, 0, len(expressions))
	schema := Schema{}
	for _, expression := range expressions {
		if _, star := expression.(*kql.StarExpression); star {
			if input.Unknown {
				items = append(items, sqlast.SelectItem{Expr: &sqlast.Star{}})
				schema.Unknown = true
				continue
			}
			for _, column := range input.Columns {
				items = append(items, sqlast.SelectItem{Expr: &sqlast.Identifier{Parts: []string{column.Name}}, Alias: column.Name})
				schema.Columns = append(schema.Columns, column)
			}
			continue
		}
		item := sqlast.SelectItem{}
		bound := boundExpr{}
		if named, ok := expression.(*kql.NamedExpression); ok {
			bound = s.expression(named.Value)
			item.Expr = bound.SQL
			item.Alias = named.Name
		} else {
			bound = s.expression(expression)
			item.Expr = bound.SQL
			if inferAlias {
				item.Alias = inferredName(expression, bound.Name)
			}
		}
		if item.Expr != nil {
			items = append(items, item)
			name := item.Alias
			if name == "" {
				name = bound.Name
			}
			if name == "" {
				name = fmt.Sprintf("Column%d", len(schema.Columns)+1)
				item.Alias = name
				items[len(items)-1].Alias = name
			}
			schema.Columns = append(schema.Columns, Column{Name: name, Type: bound.Type})
		}
	}
	s.validateUnique(schema.Columns, spanOfExpressions(expressions), "operator.project")
	return items, schema
}

func (s *compileState) expressions(expressions []kql.Expression) []sqlast.Expr {
	result := make([]sqlast.Expr, 0, len(expressions))
	for _, expression := range expressions {
		if value := s.expression(expression); value.SQL != nil {
			result = append(result, value.SQL)
		}
	}
	return result
}

func (s *compileState) expression(expression kql.Expression) boundExpr {
	if expression == nil {
		return boundExpr{}
	}
	switch value := expression.(type) {
	case *kql.NameExpression:
		if bound, ok := s.scalarLets[strings.ToLower(value.Name)]; ok {
			return bound
		}
		return s.name(value)
	case *kql.LiteralExpression:
		return s.literal(value)
	case *kql.StarExpression:
		return boundExpr{SQL: &sqlast.Star{}, Type: TypeUnknown, Name: "*"}
	case *kql.UnaryExpression:
		operand := s.expression(value.Operand)
		return boundExpr{SQL: &sqlast.Unary{Operator: value.Operator, Operand: operand.SQL}, Type: operand.Type}
	case *kql.BinaryExpression:
		return s.binary(value)
	case *kql.CallExpression:
		return s.call(value)
	case *kql.IndexExpression:
		base, index := s.expression(value.Value), s.expression(value.Index)
		if base.Type != TypeDynamic && base.Type != TypeUnknown {
			s.bindError("KQLB0405", value.Span, "expression.index", "property access requires a dynamic value")
		}
		return boundExpr{SQL: &sqlast.Index{Value: base.SQL, Index: index.SQL}, Type: TypeDynamic}
	case *kql.NamedExpression:
		return s.expression(value.Value)
	case *kql.ListExpression:
		return boundExpr{SQL: &sqlast.List{Items: s.expressions(value.Items)}, Type: TypeDynamic}
	case *kql.RawExpression:
		s.error("KQLL0401", value.Span, "expression.raw", "expression syntax is preserved but cannot be lowered")
		return boundExpr{}
	default:
		s.error("KQLL0402", expression.NodeSpan(), "expression.unknown", fmt.Sprintf("unsupported expression node %T", expression))
		return boundExpr{}
	}
}

func (s *compileState) join(input Relation, operator kql.Operator) Relation {
	spec := operator.Body.(kql.JoinSpec)
	leftAlias := s.nextAlias()
	right := s.pipeline(spec.Right)
	if right.Query == nil {
		return Relation{}
	}
	rightAlias := s.nextAlias()
	if s.compiler.limits.MaxJoinKeys > 0 && len(spec.Conditions) > s.compiler.limits.MaxJoinKeys {
		s.bindError("KQLB0312", operator.Span, "operator.join", fmt.Sprintf("join exceeds MaxJoinKeys (%d)", s.compiler.limits.MaxJoinKeys))
		return Relation{}
	}

	condition := s.joinCondition(spec, input.Schema, right.Schema, leftAlias, rightAlias, operator)
	if condition == nil && (len(spec.Conditions) > 0 || spec.Condition != nil) {
		return Relation{}
	}
	if spec.Kind == "leftanti" || spec.Kind == "leftsemi" || spec.Kind == "rightanti" || spec.Kind == "rightsemi" {
		outer, inner := input, right
		outerAlias, innerAlias := leftAlias, rightAlias
		if strings.HasPrefix(spec.Kind, "right") {
			outer, inner = right, input
			outerAlias, innerAlias = rightAlias, leftAlias
		}
		exists := sqlast.Expr(&sqlast.Exists{Query: &sqlast.Select{Projections: []sqlast.SelectItem{{Expr: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "1"}}}, From: &sqlast.Subquery{Query: inner.Query, Alias: innerAlias}, Where: condition}})
		if strings.HasSuffix(spec.Kind, "anti") {
			exists = &sqlast.Unary{Operator: "NOT ", Operand: exists}
		}
		return Relation{Query: &sqlast.Select{From: &sqlast.Subquery{Query: outer.Query, Alias: outerAlias}, Where: exists}, Schema: outer.Schema.Clone()}
	}
	if spec.Kind == "innerunique" {
		input = s.deduplicateJoinLeft(input, condition, leftAlias, rightAlias, operator)
		if input.Query == nil {
			return Relation{}
		}
	}
	join := &sqlast.Join{
		Left:  &sqlast.Subquery{Query: input.Query, Alias: leftAlias},
		Right: &sqlast.Subquery{Query: right.Query, Alias: rightAlias},
		On:    condition,
	}
	switch spec.Kind {
	case "inner", "innerunique":
		join.Kind = "INNER"
	case "leftouter":
		join.Kind = "LEFT"
	case "rightouter":
		join.Kind = "RIGHT"
	case "fullouter":
		join.Kind = "FULL"
	default:
		s.error("KQLL0311", operator.Span, "operator."+operator.Kind+"."+spec.Kind, "join kind "+spec.Kind+" requires semi/anti relational lowering")
		return Relation{}
	}
	output := joinSchema(input.Schema, right.Schema, operator.Kind == "lookup")
	var projections []sqlast.SelectItem
	if !output.Unknown {
		projections = joinProjections(input.Schema, right.Schema, output, leftAlias, rightAlias, operator.Kind == "lookup")
	}
	return Relation{Query: &sqlast.Select{From: join, Projections: projections}, Schema: output}
}

func (s *compileState) mvExpand(input Relation, spec kql.MvExpandSpec, operator kql.Operator) Relation {
	return s.expandJSON(input, spec.Items, spec.ItemIndex, spec.Limit, spec.Kind, operator)
}

func (s *compileState) mvApply(input Relation, spec kql.MvApplySpec, operator kql.Operator) Relation {
	relation := s.expandJSON(input, spec.Items, spec.ItemIndex, spec.Limit, "bag", operator)
	if relation.Query == nil {
		return Relation{}
	}
	for _, inner := range spec.Operators {
		switch inner.Kind {
		case "where", "extend", "serialize":
			relation = s.operator(relation, inner)
		default:
			s.error("KQLL0334", inner.Span, "operator.mv-apply."+inner.Kind, "SQLite mv-apply currently supports where, extend, and serialize subqueries")
			return Relation{}
		}
		if relation.Query == nil {
			return Relation{}
		}
	}
	return relation
}

func (s *compileState) expandJSON(input Relation, items []kql.MvExpansion, itemIndex string, limit kql.Expression, expansionKind string, operator kql.Operator) Relation {
	if !supportsJSONEach(s.compiler.dialect) {
		s.error("KQLL0330", operator.Span, "operator."+operator.Kind, "dialect "+s.compiler.dialect.Name()+" has no JSON array expansion")
		return Relation{}
	}
	if s.compiler.limits.MaxExpansionItems > 0 && len(items) > s.compiler.limits.MaxExpansionItems {
		s.bindError("KQLB0337", operator.Span, "operator."+operator.Kind, fmt.Sprintf("expansion exceeds MaxExpansionItems (%d)", s.compiler.limits.MaxExpansionItems))
		return Relation{}
	}
	if len(items) != 1 {
		s.error("KQLL0331", operator.Span, "operator."+operator.Kind, operator.Kind+" currently requires exactly one array expression")
		return Relation{}
	}
	item := items[0]
	sourceName := ""
	if name, ok := item.Expression.(*kql.NameExpression); ok {
		sourceName = name.Name
	}
	if item.Name == "" {
		s.error("KQLL0332", item.Span, "operator."+operator.Kind, operator.Kind+" requires an explicit output name for non-column expressions")
		return Relation{}
	}
	if input.Schema.Unknown && strings.EqualFold(item.Name, sourceName) {
		s.error("KQLL0333", item.Span, "operator."+operator.Kind, "column replacement requires a bound input schema")
		return Relation{}
	}

	inputAlias, expansionAlias := s.nextAlias(), s.nextAlias()
	previousQualifier := s.qualifier
	s.qualifier = inputAlias
	previousInput := s.input
	s.input = input.Schema
	value := s.expression(item.Expression)
	s.input = previousInput
	s.qualifier = previousQualifier
	if value.Type != TypeDynamic && value.Type != TypeUnknown {
		s.bindError("KQLB0338", item.Span, "operator."+operator.Kind, "expanded expression must be dynamic")
		return Relation{}
	}
	expansion := &sqlast.JSONEach{Value: value.SQL, Alias: expansionAlias}
	from := &sqlast.Join{
		Left:  &sqlast.Subquery{Query: input.Query, Alias: inputAlias},
		Right: expansion,
		Kind:  "CROSS",
	}
	element := sqlast.Expr(&sqlast.Identifier{Parts: []string{expansionAlias, "value"}})
	expansionType := sqlast.Expr(&sqlast.Identifier{Parts: []string{expansionAlias, "type"}})
	isNestedJSON := &sqlast.Binary{
		Left:     &sqlast.Binary{Left: expansionType, Operator: "=", Right: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "object"}},
		Operator: "OR",
		Right:    &sqlast.Binary{Left: expansionType, Operator: "=", Right: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "array"}},
	}
	jsonValue := sqlast.Expr(&sqlast.Case{Branches: []sqlast.When{{Condition: isNestedJSON, Result: &sqlast.Call{Name: "json", Args: []sqlast.Expr{element}}}}, Else: element})
	isObject := &sqlast.Binary{Left: &sqlast.Call{Name: "json_type", Args: []sqlast.Expr{value.SQL}}, Operator: "=", Right: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "object"}}
	objectElement := sqlast.Expr(&sqlast.Call{Name: "json_object", Args: []sqlast.Expr{&sqlast.Identifier{Parts: []string{expansionAlias, "key"}}, jsonValue}})
	if expansionKind == "array" {
		objectElement = &sqlast.Call{Name: "json_array", Args: []sqlast.Expr{&sqlast.Identifier{Parts: []string{expansionAlias, "key"}}, jsonValue}}
	}
	element = &sqlast.Case{Branches: []sqlast.When{{Condition: isObject, Result: objectElement}}, Else: element}
	if item.Type != "" {
		types := map[string]string{
			"long": "BIGINT", "int": "INTEGER", "real": "DOUBLE PRECISION", "double": "DOUBLE PRECISION",
			"decimal": "DECIMAL", "string": "VARCHAR", "bool": "BOOLEAN", "boolean": "BOOLEAN",
			"datetime": "TIMESTAMP", "guid": "UUID",
		}
		target, ok := types[item.Type]
		if !ok {
			s.error("KQLL0335", item.Span, "operator."+operator.Kind, "unsupported expansion type "+item.Type)
			return Relation{}
		}
		s.warn("KQLL0336", item.Span, "operator."+operator.Kind, "SQLite casts invalid expanded values instead of returning null")
		element = &sqlast.Cast{Expr: element, Type: target}
	}
	output := input.Schema.Clone()
	columnType := TypeDynamic
	if item.Type != "" {
		columnType = parseScalarType(item.Type)
	}
	_, replaceAt, replacing, _ := output.Lookup(item.Name)
	if replacing {
		output.Columns[replaceAt] = Column{Name: output.Columns[replaceAt].Name, Type: columnType}
	} else {
		output.Columns = append(output.Columns, Column{Name: item.Name, Type: columnType})
	}
	projections := make([]sqlast.SelectItem, 0, len(output.Columns)+1)
	if output.Unknown {
		projections = append(projections, sqlast.SelectItem{Expr: &sqlast.QualifiedStar{Parts: []string{inputAlias}}})
		projections = append(projections, sqlast.SelectItem{Expr: element, Alias: item.Name})
	} else {
		for _, column := range input.Schema.Columns {
			expr := sqlast.Expr(&sqlast.Identifier{Parts: []string{inputAlias, column.Name}})
			if strings.EqualFold(column.Name, item.Name) {
				expr = element
			}
			projections = append(projections, sqlast.SelectItem{Expr: expr, Alias: column.Name})
		}
		if !replacing {
			projections = append(projections, sqlast.SelectItem{Expr: element, Alias: item.Name})
		}
	}
	if itemIndex != "" {
		projections = append(projections, sqlast.SelectItem{
			Expr: &sqlast.Case{Branches: []sqlast.When{{
				Condition: &sqlast.Binary{Left: &sqlast.Call{Name: "json_type", Args: []sqlast.Expr{value.SQL}}, Operator: "=", Right: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "array"}},
				Result:    &sqlast.Cast{Expr: &sqlast.Identifier{Parts: []string{expansionAlias, "key"}}, Type: "INTEGER"},
			}}, Else: &sqlast.Literal{Kind: sqlast.NullLiteral}},
			Alias: itemIndex,
		})
		output.Columns = append(output.Columns, Column{Name: itemIndex, Type: TypeLong})
	}
	query := &sqlast.Select{From: from, Projections: projections}
	if limit != nil {
		query.Where = &sqlast.Binary{
			Left:     &sqlast.Cast{Expr: &sqlast.Identifier{Parts: []string{expansionAlias, "key"}}, Type: "INTEGER"},
			Operator: "<",
			Right:    s.expression(limit).SQL,
		}
	}
	return Relation{Query: query, Schema: output}
}

func supportsRandom(target dialect.Dialect) bool {
	capability, ok := target.(interface{ Random() (string, bool) })
	if !ok {
		return false
	}
	_, ok = capability.Random()
	return ok
}

func supportsJSONEach(target dialect.Dialect) bool {
	capability, ok := target.(interface {
		JSONEach(string, string) (string, bool)
	})
	if !ok {
		return false
	}
	_, ok = capability.JSONEach("", "")
	return ok
}

func supportsRegex(target dialect.Dialect) bool {
	_, ok := target.Regex("", "", true)
	return ok
}

func (s *compileState) binary(value *kql.BinaryExpression) boundExpr {
	op := strings.ToLower(value.Operator)
	left := s.expression(value.Left)
	right := boundExpr{}
	if !isTermOperator(op) {
		right = s.expression(value.Right)
	}
	s.use(scalarOperatorID(op), "equivalent", value.Span)
	boolean := func(expression sqlast.Expr) boundExpr { return boundExpr{SQL: expression, Type: TypeBool} }
	if isNullExpression(right.SQL) && (op == "==" || op == "!=" || op == "<>") {
		operator := "IS"
		if op != "==" {
			operator = "IS NOT"
		}
		return boolean(&sqlast.Binary{Left: left.SQL, Operator: operator, Right: right.SQL})
	}
	if isNullExpression(left.SQL) && (op == "==" || op == "!=" || op == "<>") {
		operator := "IS"
		if op != "==" {
			operator = "IS NOT"
		}
		return boolean(&sqlast.Binary{Left: right.SQL, Operator: operator, Right: left.SQL})
	}
	switch op {
	case "==":
		op = "="
	case "!=", "<>":
		op = "<>"
	case "and", "or":
		op = strings.ToUpper(op)
	case "=~", "!~":
		comparison := "="
		if op == "!~" {
			comparison = "<>"
		}
		return boolean(&sqlast.Binary{Left: &sqlast.Call{Name: "lower", Args: []sqlast.Expr{left.SQL}}, Operator: comparison, Right: &sqlast.Call{Name: "lower", Args: []sqlast.Expr{right.SQL}}})
	case "in", "!in":
		if op == "!in" {
			op = "NOT IN"
		} else {
			op = "IN"
		}
	case "in~", "!in~":
		list, ok := right.SQL.(*sqlast.List)
		if !ok || len(list.Items) == 0 {
			s.error("KQLL0403", value.Span, "operator.scalar."+op, op+" requires a non-empty scalar value list")
			return boundExpr{}
		}
		items := make([]sqlast.Expr, len(list.Items))
		for i, item := range list.Items {
			items[i] = &sqlast.Call{Name: "lower", Args: []sqlast.Expr{item}}
		}
		operator := "IN"
		if op == "!in~" {
			operator = "NOT IN"
		}
		return boolean(&sqlast.Binary{Left: &sqlast.Call{Name: "lower", Args: []sqlast.Expr{left.SQL}}, Operator: operator, Right: &sqlast.List{Items: items}})
	case "between", "!between":
		list, ok := right.SQL.(*sqlast.List)
		if !ok || len(list.Items) != 2 {
			return boundExpr{}
		}
		between := &sqlast.Binary{Left: left.SQL, Operator: "BETWEEN", Right: &sqlast.Binary{Left: list.Items[0], Operator: "AND", Right: list.Items[1]}}
		if op == "!between" {
			return boolean(&sqlast.Unary{Operator: "NOT ", Operand: between})
		}
		return boolean(between)
	case "contains", "contains_cs", "containscs", "!contains", "!contains_cs", "notcontains", "notcontainscs":
		caseSensitive := strings.Contains(op, "_cs") || strings.HasSuffix(op, "cs")
		negative := strings.HasPrefix(op, "!") || strings.HasPrefix(op, "not")
		return boolean(stringMatch(left.SQL, right.SQL, "both", caseSensitive, negative))
	case "startswith", "startswith_cs", "!startswith", "!startswith_cs":
		return boolean(stringMatch(left.SQL, right.SQL, "suffix", strings.Contains(op, "_cs"), strings.HasPrefix(op, "!")))
	case "endswith", "endswith_cs", "!endswith", "!endswith_cs":
		return boolean(stringMatch(left.SQL, right.SQL, "prefix", strings.Contains(op, "_cs"), strings.HasPrefix(op, "!")))
	case "matches regex":
		return boolean(&sqlast.Regex{Value: left.SQL, Pattern: right.SQL, CaseSensitive: true})
	case "has", "!has", "has_cs", "!has_cs", "hasprefix", "!hasprefix", "hasprefix_cs", "!hasprefix_cs", "hassuffix", "!hassuffix", "hassuffix_cs", "!hassuffix_cs":
		if !supportsRegex(s.compiler.dialect) {
			s.error("KQLL0404", value.Span, "operator.scalar."+op, op+" requires a regex-capable SQL dialect")
			return boundExpr{}
		}
		mode := "term"
		if strings.Contains(op, "prefix") {
			mode = "prefix"
		} else if strings.Contains(op, "suffix") {
			mode = "suffix"
		}
		result, ok := s.termMatchExpression(left.SQL, value.Right, mode, strings.Contains(op, "_cs"), strings.HasPrefix(op, "!"))
		if !ok {
			s.error("KQLL0404", value.Span, "operator.scalar."+op, op+" currently requires one non-empty alphanumeric literal term")
			return boundExpr{}
		}
		return boolean(result)
	case "has_any", "has_all":
		if !supportsRegex(s.compiler.dialect) {
			s.error("KQLL0404", value.Span, "operator.scalar."+op, op+" requires a regex-capable SQL dialect")
			return boundExpr{}
		}
		list, ok := value.Right.(*kql.ListExpression)
		if !ok || len(list.Items) == 0 {
			s.error("KQLL0404", value.Span, "operator.scalar."+op, op+" requires a non-empty literal term list")
			return boundExpr{}
		}
		operator := "OR"
		if op == "has_all" {
			operator = "AND"
		}
		var result sqlast.Expr
		for _, item := range list.Items {
			match, matched := s.termMatchExpression(left.SQL, item, "term", false, false)
			if !matched {
				s.error("KQLL0404", value.Span, "operator.scalar."+op, op+" currently requires non-empty alphanumeric literal terms")
				return boundExpr{}
			}
			if result == nil {
				result = match
			} else {
				result = &sqlast.Binary{Left: result, Operator: operator, Right: match}
			}
		}
		return boolean(result)
	case ":":
		s.error("KQLL0404", value.Span, "operator.scalar.colon", ": search semantics require input schema information")
		return boundExpr{}
	}
	resultType := left.Type
	if op == "=" || op == "<>" || op == "<" || op == "<=" || op == ">" || op == ">=" || op == "AND" || op == "OR" || op == "IN" || op == "NOT IN" {
		resultType = TypeBool
	} else if left.Type == TypeReal || right.Type == TypeReal {
		resultType = TypeReal
	} else if left.Type == TypeLong || right.Type == TypeLong {
		resultType = TypeLong
	}
	return boundExpr{SQL: &sqlast.Binary{Left: left.SQL, Operator: op, Right: right.SQL}, Type: resultType}
}

func isTermOperator(operator string) bool {
	switch operator {
	case "has", "!has", "has_cs", "!has_cs", "hasprefix", "!hasprefix", "hasprefix_cs", "!hasprefix_cs", "hassuffix", "!hassuffix", "hassuffix_cs", "!hassuffix_cs", "has_any", "has_all":
		return true
	default:
		return false
	}
}

func isNullExpression(expression sqlast.Expr) bool {
	literal, ok := expression.(*sqlast.Literal)
	return ok && literal.Kind == sqlast.NullLiteral
}

func (s *compileState) termMatchExpression(left sqlast.Expr, expression kql.Expression, mode string, caseSensitive, negative bool) (sqlast.Expr, bool) {
	literal, ok := expression.(*kql.LiteralExpression)
	if !ok || literal.Kind != kql.StringToken {
		return nil, false
	}
	term := unquoteKQL(literal.Text)
	if !isAlphaNumeric(term) {
		return nil, false
	}
	leftBoundary, rightBoundary := s.termBoundaries()
	pattern := leftBoundary + term + rightBoundary
	switch mode {
	case "prefix":
		pattern = leftBoundary + term + `[[:alnum:]]*`
	case "suffix":
		pattern = leftBoundary + `[[:alnum:]]*` + term + rightBoundary
	}
	result := sqlast.Expr(&sqlast.Regex{Value: left, Pattern: s.bind(pattern), CaseSensitive: caseSensitive})
	if negative {
		result = &sqlast.Unary{Operator: "NOT ", Operand: result}
	}
	return result, true
}

func (s *compileState) termBoundaries() (string, string) {
	if s.compiler.dialect.Name() == "sqlite" {
		return `(^|[^A-Za-z0-9])`, `([^A-Za-z0-9]|$)`
	}
	return `(^|[^[:alnum:]])`, `([^[:alnum:]]|$)`
}

func stringMatch(left, right sqlast.Expr, wildcard string, caseSensitive, negative bool) sqlast.Expr {
	patternArgs := []sqlast.Expr{}
	if wildcard == "both" || wildcard == "prefix" {
		patternArgs = append(patternArgs, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "%"})
	}
	patternArgs = append(patternArgs, right)
	if wildcard == "both" || wildcard == "suffix" {
		patternArgs = append(patternArgs, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "%"})
	}
	pattern := sqlast.Expr(&sqlast.Call{Name: "concat", Args: patternArgs})
	if !caseSensitive {
		left = &sqlast.Call{Name: "lower", Args: []sqlast.Expr{left}}
		pattern = &sqlast.Call{Name: "lower", Args: []sqlast.Expr{pattern}}
	}
	op := "LIKE"
	if negative {
		op = "NOT LIKE"
	}
	return &sqlast.Binary{Left: left, Operator: op, Right: pattern}
}

func termMatch(left, right sqlast.Expr, mode string, caseSensitive, negative bool) (sqlast.Expr, bool) {
	literal, ok := right.(*sqlast.Literal)
	if !ok || literal.Kind != sqlast.StringLiteral || literal.Value == "" {
		return nil, false
	}
	for _, r := range literal.Value {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return nil, false
		}
	}
	term := literal.Value
	leftBoundary, rightBoundary := `(^|[^[:alnum:]])`, `([^[:alnum:]]|$)`
	pattern := leftBoundary + term + rightBoundary
	switch mode {
	case "prefix":
		pattern = leftBoundary + term + `[[:alnum:]]*`
	case "suffix":
		pattern = leftBoundary + `[[:alnum:]]*` + term + rightBoundary
	}
	result := sqlast.Expr(&sqlast.Regex{
		Value:         left,
		Pattern:       &sqlast.Literal{Kind: sqlast.StringLiteral, Value: pattern},
		CaseSensitive: caseSensitive,
	})
	if negative {
		result = &sqlast.Unary{Operator: "NOT ", Operand: result}
	}
	return result, true
}

func lowerLiteral(value *kql.LiteralExpression) sqlast.Expr {
	text := value.Text
	if value.Kind == kql.StringToken {
		return &sqlast.Literal{Kind: sqlast.StringLiteral, Value: unquoteKQL(text)}
	}
	if strings.EqualFold(text, "true") || strings.EqualFold(text, "false") {
		return &sqlast.Literal{Kind: sqlast.BooleanLiteral, Value: strings.ToLower(text)}
	}
	if strings.EqualFold(text, "null") {
		return &sqlast.Literal{Kind: sqlast.NullLiteral}
	}
	if isTimespan(text) {
		return &sqlast.Literal{Kind: sqlast.IntervalLiteral, Value: text}
	}
	return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: text}
}

func (s *compileState) call(value *kql.CallExpression) boundExpr {
	name, ok := value.Function.(*kql.NameExpression)
	if !ok {
		s.error("KQLL0501", value.Span, "function.dynamic", "dynamic function references are not supported")
		return boundExpr{}
	}
	function := strings.ToLower(name.Name)
	if function == "datetime" || function == "date" || function == "timespan" || function == "time" {
		if len(value.Args) != 1 {
			return s.arity(value, function, 1)
		}
		text := expressionText(value.Args[0])
		if s.compiler.parameterization == BoundParameters {
			resultType := TypeUnknown
			if function == "datetime" || function == "date" {
				resultType = TypeDateTime
			}
			return boundExpr{SQL: s.bind(text), Type: resultType}
		}
		kind, resultType := sqlast.IntervalLiteral, TypeUnknown
		if function == "datetime" || function == "date" {
			kind, resultType = sqlast.DateTimeLiteral, TypeDateTime
		}
		return boundExpr{SQL: &sqlast.Literal{Kind: kind, Value: text}, Type: resultType}
	}
	args := make([]sqlast.Expr, 0, len(value.Args))
	argTypes := make([]ScalarType, 0, len(value.Args))
	for _, argument := range value.Args {
		bound := s.expression(argument)
		if bound.SQL != nil {
			args = append(args, bound.SQL)
			argTypes = append(argTypes, bound.Type)
		}
	}
	s.use("function."+function, "equivalent", value.Span)
	if rule, ok := s.compiler.typedFunctionRules[function]; ok {
		typed := make([]TypedExpression, len(args))
		for i := range args {
			typed[i] = TypedExpression{SQL: args[i], Type: argTypes[i]}
		}
		result, err := rule(typed)
		if err != nil {
			s.error("KQLL0504", value.Span, "function."+function, err.Error())
			return boundExpr{}
		}
		if result.SQL == nil {
			s.error("KQLL0505", value.Span, "function."+function, "custom function rule returned a nil expression")
			return boundExpr{}
		}
		if result.Type == "" {
			result.Type = TypeUnknown
		}
		return boundExpr{SQL: result.SQL, Type: result.Type}
	}
	if rule, ok := s.compiler.functionRules[function]; ok {
		result, err := rule(args)
		if err != nil {
			s.error("KQLL0504", value.Span, "function."+function, err.Error())
			return boundExpr{}
		}
		if result == nil {
			s.error("KQLL0505", value.Span, "function."+function, "custom function rule returned a nil expression")
		}
		return boundExpr{SQL: result, Type: functionType(function, argTypes)}
	}
	switch function {
	case "not":
		if len(args) != 1 {
			return s.arity(value, function, 1)
		}
		if argTypes[0] != TypeBool && argTypes[0] != TypeUnknown {
			s.bindError("KQLB0502", value.Span, "function.not", "not requires a Boolean expression")
			return boundExpr{}
		}
		return boundExpr{SQL: &sqlast.Unary{Operator: "NOT ", Operand: args[0]}, Type: TypeBool}

	case "iif", "iff":
		if len(args) != 3 {
			return s.arity(value, function, 3)
		}
		return boundExpr{SQL: &sqlast.Case{Branches: []sqlast.When{{Condition: args[0], Result: args[1]}}, Else: args[2]}, Type: argTypes[1]}
	case "case":
		if len(args) < 3 || len(args)%2 == 0 {
			s.error("KQLB0502", value.Span, "function.case", "case requires condition/result pairs and an else expression")
			return boundExpr{}
		}
		result := &sqlast.Case{Else: args[len(args)-1]}
		for i := 0; i < len(args)-1; i += 2 {
			result.Branches = append(result.Branches, sqlast.When{Condition: args[i], Result: args[i+1]})
		}
		return boundExpr{SQL: result, Type: argTypes[len(argTypes)-1]}
	case "int", "int32", "long", "int64", "real", "double", "decimal":
		if len(args) != 1 {
			return s.arity(value, function, 1)
		}
		types := map[string]string{"int": "INTEGER", "int32": "INTEGER", "long": "BIGINT", "int64": "BIGINT", "real": "DOUBLE PRECISION", "double": "DOUBLE PRECISION", "decimal": "DECIMAL"}
		return boundExpr{SQL: &sqlast.Cast{Expr: args[0], Type: types[function]}, Type: functionType(function, argTypes)}
	case "tostring":
		if len(args) != 1 {
			return s.arity(value, function, 1)
		}
		return boundExpr{SQL: &sqlast.Cast{Expr: args[0], Type: "VARCHAR"}, Type: TypeString}
	case "toint", "tolong", "toreal", "todouble", "todatetime", "todecimal", "tobool", "toboolean", "toguid":
		if len(args) != 1 {
			return s.arity(value, function, 1)
		}
		types := map[string]string{"toint": "INTEGER", "tolong": "BIGINT", "toreal": "DOUBLE PRECISION", "todouble": "DOUBLE PRECISION", "todatetime": "TIMESTAMP", "todecimal": "DECIMAL", "tobool": "BOOLEAN", "toboolean": "BOOLEAN", "toguid": "UUID"}
		return boundExpr{SQL: &sqlast.Cast{Expr: args[0], Type: types[function], Safe: true}, Type: functionType(function, argTypes)}
	case "strcat":
		return boundExpr{SQL: &sqlast.Call{Name: "concat", Args: args}, Type: TypeString}
	case "strlen":
		return boundExpr{SQL: &sqlast.Call{Name: "length", Args: args}, Type: TypeLong}
	case "tolower":
		return boundExpr{SQL: &sqlast.Call{Name: "lower", Args: args}, Type: TypeString}
	case "toupper":
		return boundExpr{SQL: &sqlast.Call{Name: "upper", Args: args}, Type: TypeString}
	case "substring":
		return boundExpr{SQL: &sqlast.Call{Name: "substr", Args: args}, Type: TypeString}
	case "countif":
		if len(args) != 1 {
			return s.arity(value, function, 1)
		}
		return boundExpr{SQL: &sqlast.Call{Name: "sum", Args: []sqlast.Expr{&sqlast.Case{Branches: []sqlast.When{{Condition: args[0], Result: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "1"}}}, Else: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "0"}}}}, Type: TypeLong}
	case "sumif":
		if len(args) != 2 {
			return s.arity(value, function, 2)
		}
		return boundExpr{SQL: &sqlast.Call{Name: "sum", Args: []sqlast.Expr{&sqlast.Case{Branches: []sqlast.When{{Condition: args[1], Result: args[0]}}, Else: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "0"}}}}, Type: functionType(function, argTypes)}
	case "count":
		if len(args) == 0 {
			args = []sqlast.Expr{&sqlast.Star{}}
		}
		return boundExpr{SQL: &sqlast.Call{Name: function, Args: args}, Type: TypeLong}
	case "make_list":
		if len(args) != 1 {
			return s.arity(value, function, 1)
		}
		name := "json_group_array"
		if s.compiler.dialect.Name() == "postgresql" {
			name = "json_agg"
		} else if s.compiler.dialect.Name() != "sqlite" {
			s.error("KQLL0503", value.Span, "aggregate.make_list", "make_list requires an explicit dialect mapping")
			return boundExpr{}
		}
		call := &sqlast.Call{Name: name, Args: args}
		return boundExpr{SQL: &sqlast.Filtered{Expr: call, Where: &sqlast.Binary{Left: args[0], Operator: "IS NOT", Right: &sqlast.Literal{Kind: sqlast.NullLiteral}}}, Type: TypeDynamic}
	case "dcount":
		s.error("KQLL0510", value.Span, "aggregate.dcount", "dcount is approximate in KQL and requires an explicit compatible aggregate mapping")
		return boundExpr{}
	case "arg_min", "arg_max":
		s.error("KQLL0511", value.Span, "aggregate."+function, function+" requires schema-aware window lowering and is not enabled")
		return boundExpr{}
	case "sum", "avg", "min", "max", "coalesce", "abs", "ceiling", "ceil", "floor", "round", "pow", "sqrt", "exp", "ln", "log", "log10", "sin", "cos", "tan", "asin", "acos", "atan", "atan2", "sign":
		return boundExpr{SQL: &sqlast.Call{Name: function, Args: args}, Type: functionType(function, argTypes)}
	default:
		s.error("KQLL0503", value.Span, "function."+function, "function "+function+" is recognized syntactically but has no safe SQL mapping")
		return boundExpr{}
	}
}

func (s *compileState) arity(value *kql.CallExpression, function string, expected int) boundExpr {
	s.error("KQLB0501", value.Span, "function."+function, fmt.Sprintf("%s requires %d arguments", function, expected))
	return boundExpr{}
}

func functionType(function string, arguments []ScalarType) ScalarType {
	switch function {
	case "int", "int32", "toint":
		return TypeInt
	case "long", "int64", "tolong", "strlen", "count", "countif":
		return TypeLong
	case "real", "double", "decimal", "toreal", "todouble", "todecimal", "avg":
		return TypeReal
	case "tostring", "strcat", "tolower", "toupper", "substring", "toguid":
		return TypeString
	case "datetime", "date", "todatetime":
		return TypeDateTime
	case "not", "tobool", "toboolean":
		return TypeBool
	case "make_list", "make_set", "make_bag", "make_bag_if", "make_dictionary", "parse_json", "parsejson":
		return TypeDynamic
	case "sqrt", "exp", "ln", "log", "log10", "sin", "cos", "tan", "asin", "acos", "atan", "atan2", "pow":
		return TypeReal
	}
	if len(arguments) > 0 {
		if (function == "sum" || function == "sumif") && arguments[0] == TypeInt {
			return TypeLong
		}
		switch function {
		case "sum", "sumif", "min", "max", "coalesce", "abs", "ceiling", "ceil", "floor", "round", "sign":
			return arguments[0]
		}
	}
	return TypeUnknown
}

func (s *compileState) nextAlias() string {
	alias := fmt.Sprintf("_k%d", s.alias)
	s.alias++
	return alias
}

func (s *compileState) use(id, support string, span kql.Span) {
	if entry, ok := featureledger.Lookup(id); ok {
		support = string(entry.Support)
	} else if strings.HasPrefix(id, "function.") {
		if entry, found := featureledger.Lookup("aggregate." + strings.TrimPrefix(id, "function.")); found {
			id = entry.ID
			support = string(entry.Support)
		}
	}
	s.features = append(s.features, FeatureUse{ID: id, Support: support, Span: span})
}

func (s *compileState) error(code string, span kql.Span, feature, message string) {
	s.diagnostics = append(s.diagnostics, Diagnostic{Code: code, Severity: SeverityError, Phase: PhaseLower, Message: message, Span: span, Feature: feature})
}

func (s *compileState) warn(code string, span kql.Span, feature, message string) {
	s.diagnostics = append(s.diagnostics, Diagnostic{Code: code, Severity: SeverityWarning, Phase: PhaseLower, Message: message, Span: span, Feature: feature})
}

func splitName(name string) []string {
	parts := strings.Split(name, ".")
	for i := range parts {
		parts[i] = unquoteKQL(parts[i])
	}
	return parts
}

func lastName(name string) string {
	parts := splitName(name)
	return parts[len(parts)-1]
}

func unquoteKQL(value string) string {
	hidden := false
	if len(value) > 1 && (value[0] == 'h' || value[0] == 'H') && (value[1] == '\'' || value[1] == '"' || value[1] == '@') {
		hidden = true
		value = value[1:]
	}
	_ = hidden
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

func isTimespan(value string) bool {
	if value == "" {
		return false
	}
	last := value[len(value)-1]
	return last >= 'a' && last <= 'z' || last >= 'A' && last <= 'Z'
}

func expressionText(expression kql.Expression) string {
	switch value := expression.(type) {
	case *kql.LiteralExpression:
		return unquoteKQL(value.Text)
	case *kql.NameExpression:
		return value.Name
	default:
		return ""
	}
}

func scalarOperatorID(operator string) string {
	names := map[string]string{
		"+": "add", "-": "subtract", "*": "multiply", "/": "divide", "%": "modulo",
		"<": "less_than", "<=": "less_than_or_equal", ">": "greater_than", ">=": "greater_than_or_equal",
		"==": "equal", "!=": "not_equal", "<>": "not_equal", "=~": "equal_tilde", "!~": "bang_tilde",
		"!has": "not_has", "!has_cs": "not_has_cs", "!hasprefix": "not_hasprefix",
		"!hasprefix_cs": "not_hasprefix_cs", "!hassuffix": "not_hassuffix",
		"!hassuffix_cs": "not_hassuffix_cs", "!contains": "not_contains",
		"!contains_cs": "not_contains_cs", "!startswith": "not_startswith",
		"!startswith_cs": "not_startswith_cs", "!endswith": "not_endswith",
		"!endswith_cs": "not_endswith_cs", "matches regex": "matches_regex",
		"!in": "not_in", "in~": "in_cs", "!in~": "not_in_cs", "!between": "not_between",
	}
	if name, ok := names[operator]; ok {
		return "operator.scalar." + name
	}
	return "operator.scalar." + strings.ReplaceAll(operator, " ", "_")
}
