package ksql

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

type loweringContext struct {
	state *compileState
	input Schema
	span  kql.Span
}

func (c *loweringContext) LowerExpression(expression kql.Expression) (sqlast.Expr, error) {
	previous := c.state.input
	c.state.input = c.input
	value := c.state.expression(expression)
	c.state.input = previous
	if value.SQL == nil {
		return nil, fmt.Errorf("expression could not be lowered")
	}
	return value.SQL, nil
}

func (c *loweringContext) LowerPipeline(pipeline *kql.Pipeline) (Relation, error) {
	relation := c.state.pipeline(pipeline)
	if relation.Query == nil {
		return Relation{}, fmt.Errorf("pipeline could not be lowered")
	}
	return relation, nil
}

func (c *loweringContext) Bind(value any) sqlast.Expr { return c.state.bind(value) }
func (c *loweringContext) InputSchema() Schema        { return c.input.Clone() }
func (c *loweringContext) NextAlias() string          { return c.state.nextAlias() }
func (c *loweringContext) Dialect() dialect.Dialect   { return c.state.compiler.dialect }
func (c *loweringContext) Catalog() Catalog           { return c.state.compiler.catalog }
func (c *loweringContext) AddDiagnostic(diagnostic Diagnostic) {
	if diagnostic.Span.Length == 0 {
		diagnostic.Span = c.span
	}
	if diagnostic.Phase == "" {
		diagnostic.Phase = PhaseLower
	}
	if diagnostic.Severity == SeverityInfo {
		diagnostic.Severity = SeverityError
	}
	c.state.diagnostics = append(c.state.diagnostics, diagnostic)
}

func (s *compileState) bind(value any) sqlast.Expr {
	if s.compiler.parameterization == BoundParameters {
		s.args = append(s.args, value)
		return &sqlast.Parameter{Index: len(s.args), Kind: valueKind(value)}
	}
	return valueLiteral(value)
}

func valueKind(value any) sqlast.LiteralKind {
	if value == nil {
		return sqlast.NullLiteral
	}
	switch reflect.TypeOf(value).Kind() {
	case reflect.Bool:
		return sqlast.BooleanLiteral
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return sqlast.NumberLiteral
	default:
		return sqlast.StringLiteral
	}
}

func valueLiteral(value any) sqlast.Expr {
	if value == nil {
		return &sqlast.Literal{Kind: sqlast.NullLiteral}
	}
	switch value := value.(type) {
	case string:
		return &sqlast.Literal{Kind: sqlast.StringLiteral, Value: value}
	case bool:
		return &sqlast.Literal{Kind: sqlast.BooleanLiteral, Value: strconv.FormatBool(value)}
	case int:
		return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: strconv.Itoa(value)}
	case int64:
		return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: strconv.FormatInt(value, 10)}
	case float64:
		return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: strconv.FormatFloat(value, 'g', -1, 64)}
	}
	valueOf := reflect.ValueOf(value)
	if valueOf.IsValid() {
		switch valueOf.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: strconv.FormatInt(valueOf.Int(), 10)}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: strconv.FormatUint(valueOf.Uint(), 10)}
		case reflect.Float32, reflect.Float64:
			return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: strconv.FormatFloat(valueOf.Float(), 'g', -1, 64)}
		}
	}
	return &sqlast.Literal{Kind: sqlast.StringLiteral, Value: fmt.Sprint(value)}
}

func (s *compileState) literal(value *kql.LiteralExpression) boundExpr {
	text := value.Text
	if value.Kind == kql.StringToken {
		return boundExpr{SQL: s.bind(unquoteKQL(text)), Type: TypeString}
	}
	if strings.EqualFold(text, "true") || strings.EqualFold(text, "false") {
		parsed := strings.EqualFold(text, "true")
		return boundExpr{SQL: s.bind(parsed), Type: TypeBool}
	}
	if strings.EqualFold(text, "null") {
		return boundExpr{SQL: &sqlast.Literal{Kind: sqlast.NullLiteral}, Type: TypeUnknown}
	}
	if isTimespan(text) {
		if s.compiler.parameterization == BoundParameters {
			return boundExpr{SQL: s.bind(text), Type: TypeUnknown}
		}
		return boundExpr{SQL: &sqlast.Literal{Kind: sqlast.IntervalLiteral, Value: text}, Type: TypeUnknown}
	}
	if strings.ContainsAny(text, ".eE") {
		parsed, err := strconv.ParseFloat(text, 64)
		if err == nil {
			return boundExpr{SQL: s.bind(parsed), Type: TypeReal}
		}
		return boundExpr{SQL: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: text}, Type: TypeReal}
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err == nil {
		return boundExpr{SQL: s.bind(parsed), Type: TypeLong}
	}
	return boundExpr{SQL: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: text}, Type: TypeLong}
}

func (s *compileState) name(value *kql.NameExpression) boundExpr {
	parts := splitName(unquoteKQL(value.Name))
	if len(parts) > 1 && s.qualifiers != nil {
		if scope, ok := s.qualifiers[strings.ToLower(parts[0])]; ok {
			return s.qualifiedName(value, scope, parts[1:])
		}
	}
	if !s.input.Unknown {
		if column, _, ok, ambiguous := s.input.Lookup(value.Name); ambiguous {
			s.bindError("KQLB0402", value.Span, "expression.name", "ambiguous column "+value.Name)
			return boundExpr{}
		} else if ok {
			return boundExpr{SQL: s.columnIdentifier(column.Name), Type: column.Type, Name: column.Name}
		}
		if len(parts) > 1 {
			if column, _, ok, ambiguous := s.input.Lookup(parts[0]); ambiguous {
				s.bindError("KQLB0402", value.Span, "expression.name", "ambiguous column "+parts[0])
				return boundExpr{}
			} else if ok {
				if column.Type != TypeDynamic {
					s.bindError("KQLB0403", value.Span, "expression.name", column.Name+" is not dynamic")
					return boundExpr{}
				}
				expr := s.columnIdentifier(column.Name)
				for _, property := range parts[1:] {
					expr = &sqlast.Index{Value: expr, Index: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: property}}
				}
				return boundExpr{SQL: expr, Type: TypeDynamic, Name: parts[len(parts)-1]}
			}
		}
		s.bindError("KQLB0401", value.Span, "expression.name", "unknown column "+value.Name)
		return boundExpr{}
	}
	if len(parts) == 1 && s.qualifier != "" {
		parts = append([]string{s.qualifier}, parts...)
	}
	return boundExpr{SQL: &sqlast.Identifier{Parts: parts}, Type: TypeUnknown, Name: lastName(value.Name)}
}

func (s *compileState) qualifiedName(value *kql.NameExpression, scope relationScope, parts []string) boundExpr {
	if len(parts) == 0 {
		s.bindError("KQLB0401", value.Span, "expression.name", "missing qualified column")
		return boundExpr{}
	}
	if scope.schema.Unknown {
		return boundExpr{SQL: &sqlast.Identifier{Parts: []string{scope.alias, parts[0]}}, Type: TypeUnknown, Name: parts[0]}
	}
	column, _, ok, ambiguous := scope.schema.Lookup(parts[0])
	if ambiguous {
		s.bindError("KQLB0402", value.Span, "expression.name", "ambiguous column "+parts[0])
		return boundExpr{}
	}
	if !ok {
		s.bindError("KQLB0401", value.Span, "expression.name", "unknown column "+parts[0])
		return boundExpr{}
	}
	expr := sqlast.Expr(&sqlast.Identifier{Parts: []string{scope.alias, column.Name}})
	if len(parts) > 1 {
		if column.Type != TypeDynamic {
			s.bindError("KQLB0403", value.Span, "expression.name", column.Name+" is not dynamic")
			return boundExpr{}
		}
		for _, property := range parts[1:] {
			expr = &sqlast.Index{Value: expr, Index: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: property}}
		}
		return boundExpr{SQL: expr, Type: TypeDynamic, Name: parts[len(parts)-1]}
	}
	return boundExpr{SQL: expr, Type: column.Type, Name: column.Name}
}

func (s *compileState) columnIdentifier(name string) sqlast.Expr {
	parts := []string{name}
	if s.qualifier != "" {
		parts = append([]string{s.qualifier}, parts...)
	}
	return &sqlast.Identifier{Parts: parts}
}

func (s *compileState) extend(from *sqlast.Subquery, input Schema, expressions []kql.Expression, operator kql.Operator) Relation {
	if !s.checkProjectionLimit(expressions, operator) {
		return Relation{}
	}
	if input.Unknown {
		items, output := s.projectItems(expressions, true, input)
		return Relation{Query: &sqlast.Select{From: from, Projections: append([]sqlast.SelectItem{{Expr: &sqlast.Star{}}}, items...)}, Schema: Schema{Unknown: true, Columns: output.Columns}}
	}
	type extension struct {
		name  string
		value boundExpr
		span  kql.Span
	}
	values := make([]extension, 0, len(expressions))
	seen := make(map[string]struct{})
	for _, expression := range expressions {
		name := ""
		valueExpression := expression
		if named, ok := expression.(*kql.NamedExpression); ok {
			name, valueExpression = named.Name, named.Value
		}
		value := s.expression(valueExpression)
		if name == "" {
			name = inferredName(valueExpression, value.Name)
		}
		if name == "" {
			s.bindError("KQLB0404", expression.NodeSpan(), "operator.extend", "extend expression requires an output name")
			continue
		}
		key := strings.ToLower(name)
		if _, duplicate := seen[key]; duplicate {
			s.bindError("KQLB0406", expression.NodeSpan(), "operator.extend", "duplicate output column "+name)
			continue
		}
		seen[key] = struct{}{}
		values = append(values, extension{name: name, value: value, span: expression.NodeSpan()})
	}
	output := input.Clone()
	for _, extension := range values {
		if column, index, ok, ambiguous := output.Lookup(extension.name); ambiguous {
			s.bindError("KQLB0402", extension.span, "operator.extend", "ambiguous column "+extension.name)
		} else if ok {
			output.Columns[index] = Column{Name: column.Name, Type: extension.value.Type}
		} else {
			output.Columns = append(output.Columns, Column{Name: extension.name, Type: extension.value.Type})
		}
	}
	items := make([]sqlast.SelectItem, 0, len(output.Columns))
	for _, column := range output.Columns {
		expr := s.columnIdentifier(column.Name)
		for _, extension := range values {
			if strings.EqualFold(column.Name, extension.name) {
				expr = extension.value.SQL
				break
			}
		}
		items = append(items, sqlast.SelectItem{Expr: expr, Alias: column.Name})
	}
	return Relation{Query: &sqlast.Select{From: from, Projections: items}, Schema: output}
}

func (s *compileState) projectionOperator(from *sqlast.Subquery, input Schema, operator kql.Operator) Relation {
	if input.Unknown {
		s.bindError("KQLB0340", operator.Span, "operator."+operator.Kind, operator.Kind+" requires a bound input schema")
		return Relation{}
	}
	spec := operator.Body.(kql.ExpressionSpec)
	if !s.checkProjectionLimit(spec.Expressions, operator) {
		return Relation{}
	}
	requested := make([]string, 0, len(spec.Expressions))
	renames := make(map[string]string)
	for _, expression := range spec.Expressions {
		if named, ok := expression.(*kql.NamedExpression); ok && operator.Kind == "project-rename" {
			old, ok := named.Value.(*kql.NameExpression)
			if !ok || strings.Contains(old.Name, ".") {
				s.bindError("KQLB0341", expression.NodeSpan(), "operator."+operator.Kind, "project-rename requires new=old column names")
				continue
			}
			column, _, found, ambiguous := input.Lookup(old.Name)
			if ambiguous || !found {
				s.bindError("KQLB0401", old.Span, "operator."+operator.Kind, "unknown column "+old.Name)
				continue
			}
			renames[strings.ToLower(column.Name)] = named.Name
			continue
		}
		name, ok := expression.(*kql.NameExpression)
		if !ok || strings.ContainsAny(name.Name, "*") {
			s.bindError("KQLB0342", expression.NodeSpan(), "operator."+operator.Kind, "only exact bound column names are supported")
			continue
		}
		column, _, found, ambiguous := input.Lookup(name.Name)
		if ambiguous || !found {
			s.bindError("KQLB0401", name.Span, "operator."+operator.Kind, "unknown column "+name.Name)
			continue
		}
		requested = append(requested, column.Name)
	}
	output := Schema{}
	switch operator.Kind {
	case "project-away":
		removed := nameSet(requested)
		for _, column := range input.Columns {
			if _, ok := removed[strings.ToLower(column.Name)]; !ok {
				output.Columns = append(output.Columns, column)
			}
		}
	case "project-keep":
		kept := nameSet(requested)
		for _, column := range input.Columns {
			if _, ok := kept[strings.ToLower(column.Name)]; ok {
				output.Columns = append(output.Columns, column)
			}
		}
	case "project-reorder":
		used := nameSet(requested)
		for _, name := range requested {
			column, _, _, _ := input.Lookup(name)
			output.Columns = append(output.Columns, column)
		}
		for _, column := range input.Columns {
			if _, ok := used[strings.ToLower(column.Name)]; !ok {
				output.Columns = append(output.Columns, column)
			}
		}
	case "project-rename":
		for _, column := range input.Columns {
			if replacement, ok := renames[strings.ToLower(column.Name)]; ok {
				column.Name = replacement
			}
			output.Columns = append(output.Columns, column)
		}
	}
	if len(output.Columns) == 0 {
		s.bindError("KQLB0343", operator.Span, "operator."+operator.Kind, "projection cannot remove every column")
		return Relation{}
	}
	s.validateUnique(output.Columns, operator.Span, "operator."+operator.Kind)
	items := make([]sqlast.SelectItem, 0, len(output.Columns))
	for i, column := range output.Columns {
		sourceName := column.Name
		if operator.Kind == "project-rename" {
			sourceName = input.Columns[i].Name
		}
		items = append(items, sqlast.SelectItem{Expr: &sqlast.Identifier{Parts: []string{sourceName}}, Alias: column.Name})
	}
	return Relation{Query: &sqlast.Select{From: from, Projections: items}, Schema: output}
}

func (s *compileState) search(from *sqlast.Subquery, input Schema, operator kql.Operator) Relation {
	if input.Unknown {
		s.bindError("KQLB0350", operator.Span, "operator.search", "search requires a bound input schema")
		return Relation{}
	}
	if maximum := s.compiler.limits.MaxProjectionItems; maximum > 0 && len(input.Columns) > maximum {
		s.bindError("KQLB0355", operator.Span, "operator.search", fmt.Sprintf("search input exceeds MaxProjectionItems (%d)", maximum))
		return Relation{}
	}
	spec, ok := operator.Body.(kql.SearchSpec)
	if !ok {
		s.error("KQLL0350", operator.Span, "operator.search", "unsupported search form")
		return Relation{}
	}
	literal, ok := spec.Term.(*kql.LiteralExpression)
	if !ok || literal.Kind != kql.StringToken {
		s.bindError("KQLB0351", spec.Term.NodeSpan(), "operator.search", "search currently requires one literal term")
		return Relation{}
	}
	term := unquoteKQL(literal.Text)
	if term == "" {
		s.bindError("KQLB0352", literal.Span, "operator.search", "search term must be non-empty")
		return Relation{}
	}
	leftBoundary, rightBoundary := s.termBoundaries()
	pattern := leftBoundary + regexp.QuoteMeta(term) + rightBoundary
	if s.compiler.limits.MaxRegexBytes > 0 && len(pattern) > s.compiler.limits.MaxRegexBytes {
		s.bindError("KQLB0353", literal.Span, "operator.search", fmt.Sprintf("search pattern exceeds MaxRegexBytes (%d)", s.compiler.limits.MaxRegexBytes))
		return Relation{}
	}
	if !supportsRegex(s.compiler.dialect) {
		s.error("KQLL0404", literal.Span, "operator.search", "search requires a term-match or regex-capable SQL dialect")
		return Relation{}
	}
	var predicate sqlast.Expr
	for _, column := range input.Columns {
		value := sqlast.Expr(&sqlast.Identifier{Parts: []string{column.Name}})
		if column.Type != TypeString && column.Type != TypeDynamic && column.Type != TypeUnknown {
			value = &sqlast.Cast{Expr: value, Type: "VARCHAR"}
		}
		match := &sqlast.Regex{Value: value, Pattern: s.bind(pattern), CaseSensitive: false}
		if predicate == nil {
			predicate = match
		} else {
			predicate = &sqlast.Binary{Left: predicate, Operator: "OR", Right: match}
		}
	}
	if predicate == nil {
		s.bindError("KQLB0354", operator.Span, "operator.search", "search input has no visible columns")
		return Relation{}
	}
	return Relation{Query: &sqlast.Select{From: from, Where: predicate}, Schema: input.Clone()}
}

func (s *compileState) joinCondition(spec kql.JoinSpec, left, right Schema, leftAlias, rightAlias string, operator kql.Operator) sqlast.Expr {
	previousQualifiers, previousInput := s.qualifiers, s.input
	s.qualifiers = map[string]relationScope{
		"$left":  {alias: leftAlias, schema: left},
		"$right": {alias: rightAlias, schema: right},
	}
	s.input = Schema{Unknown: true}
	defer func() { s.qualifiers, s.input = previousQualifiers, previousInput }()
	var result sqlast.Expr
	for _, expression := range spec.Conditions {
		if name, ok := expression.(*kql.NameExpression); ok && !strings.Contains(name.Name, ".") {
			leftColumn, _, leftOK, leftAmbiguous := left.Lookup(name.Name)
			rightColumn, _, rightOK, rightAmbiguous := right.Lookup(name.Name)
			if !left.Unknown && (leftAmbiguous || !leftOK) || !right.Unknown && (rightAmbiguous || !rightOK) {
				s.bindError("KQLB0310", name.Span, "operator.join", "invalid join key "+name.Name)
				return nil
			}
			leftName, rightName := name.Name, name.Name
			if leftOK {
				leftName = leftColumn.Name
			}
			if rightOK {
				rightName = rightColumn.Name
			}
			if leftOK && rightOK && leftColumn.Type != TypeUnknown && rightColumn.Type != TypeUnknown && leftColumn.Type != rightColumn.Type {
				s.bindError("KQLB0311", name.Span, "operator.join", "join key types do not match for "+name.Name)
				return nil
			}
			condition := &sqlast.Binary{Left: &sqlast.Identifier{Parts: []string{leftAlias, leftName}}, Operator: "=", Right: &sqlast.Identifier{Parts: []string{rightAlias, rightName}}}
			result = andExpression(result, condition)
			continue
		}
		condition := s.expression(expression)
		result = andExpression(result, condition.SQL)
	}
	if spec.Condition != nil {
		condition := s.expression(spec.Condition)
		result = andExpression(result, condition.SQL)
	}
	return result
}

func joinSchema(left, right Schema, omitRightKeys bool) Schema {
	if left.Unknown || right.Unknown {
		return Schema{Unknown: true}
	}
	result := left.Clone()
	used := nameSetFromColumns(result.Columns)
	for _, column := range right.Columns {
		if omitRightKeys {
			if _, _, found, _ := left.Lookup(column.Name); found {
				continue
			}
		}
		base := column.Name
		if _, collision := used[strings.ToLower(column.Name)]; collision {
			for suffix := 1; ; suffix++ {
				candidate := base + strconv.Itoa(suffix)
				if _, exists := used[strings.ToLower(candidate)]; !exists {
					column.Name = candidate
					break
				}
			}
		}
		used[strings.ToLower(column.Name)] = struct{}{}
		result.Columns = append(result.Columns, column)
	}
	return result
}

func joinProjections(left, right, output Schema, leftAlias, rightAlias string, omitRightKeys bool) []sqlast.SelectItem {
	items := make([]sqlast.SelectItem, 0, len(output.Columns))
	for _, column := range left.Columns {
		items = append(items, sqlast.SelectItem{Expr: &sqlast.Identifier{Parts: []string{leftAlias, column.Name}}, Alias: column.Name})
	}
	used := nameSetFromColumns(left.Columns)
	for _, column := range right.Columns {
		if omitRightKeys {
			if _, _, found, _ := left.Lookup(column.Name); found {
				continue
			}
		}
		alias := column.Name
		if _, collision := used[strings.ToLower(alias)]; collision {
			for suffix := 1; ; suffix++ {
				candidate := column.Name + strconv.Itoa(suffix)
				if _, exists := used[strings.ToLower(candidate)]; !exists {
					alias = candidate
					break
				}
			}
		}
		used[strings.ToLower(alias)] = struct{}{}
		items = append(items, sqlast.SelectItem{Expr: &sqlast.Identifier{Parts: []string{rightAlias, column.Name}}, Alias: alias})
	}
	return items
}

func (s *compileState) validateUnique(columns []Column, span kql.Span, feature string) bool {
	seen := make(map[string]struct{}, len(columns))
	valid := true
	for _, column := range columns {
		key := strings.ToLower(column.Name)
		if _, duplicate := seen[key]; duplicate {
			s.bindError("KQLB0406", span, feature, "duplicate output column "+column.Name)
			valid = false
		}
		seen[key] = struct{}{}
	}
	return valid
}

func (s *compileState) checkProjectionLimit(expressions []kql.Expression, operator kql.Operator) bool {
	limit := s.compiler.limits.MaxProjectionItems
	if limit > 0 && len(expressions) > limit {
		s.bindError("KQLB0344", operator.Span, "operator."+operator.Kind, fmt.Sprintf("projection exceeds MaxProjectionItems (%d)", limit))
		return false
	}
	return true
}

func (s *compileState) validateLimit(expression kql.Expression, operator kql.Operator) bool {
	literal, ok := expression.(*kql.LiteralExpression)
	if !ok || literal.Kind != kql.NumberToken || strings.ContainsAny(literal.Text, ".eE") {
		s.bindError("KQLB0302", expression.NodeSpan(), "operator."+operator.Kind, "row limit must be a constant integer")
		return false
	}
	value, err := strconv.ParseInt(literal.Text, 10, 64)
	if err != nil || value < 0 {
		s.bindError("KQLB0302", expression.NodeSpan(), "operator."+operator.Kind, "row limit must be a non-negative bounded integer")
		return false
	}
	if maximum := s.compiler.limits.MaxOutputRows; maximum > 0 && value > int64(maximum) {
		s.bindError("KQLB0303", expression.NodeSpan(), "operator."+operator.Kind, fmt.Sprintf("row limit exceeds MaxOutputRows (%d)", maximum))
		return false
	}
	return true
}

func (s *compileState) bindError(code string, span kql.Span, feature, message string) {
	s.diagnostics = append(s.diagnostics, Diagnostic{Code: code, Severity: SeverityError, Phase: PhaseBind, Message: message, Span: span, Feature: feature})
}

func inferredName(expression kql.Expression, boundName string) string {
	if boundName != "" {
		return boundName
	}
	switch value := expression.(type) {
	case *kql.NameExpression:
		return lastName(value.Name)
	case *kql.CallExpression:
		if function, ok := value.Function.(*kql.NameExpression); ok {
			return lastName(function.Name) + "_"
		}
	}
	return ""
}

func parseScalarType(value string) ScalarType {
	switch strings.ToLower(value) {
	case "bool", "boolean":
		return TypeBool
	case "int", "int32", "long", "int64":
		return TypeLong
	case "real", "double", "decimal":
		return TypeReal
	case "string", "guid":
		return TypeString
	case "datetime", "date":
		return TypeDateTime
	case "dynamic":
		return TypeDynamic
	default:
		return TypeUnknown
	}
}

func schemasEqual(left, right Schema) bool {
	if left.Unknown || right.Unknown {
		return true
	}
	if len(left.Columns) != len(right.Columns) {
		return false
	}
	for i := range left.Columns {
		if !strings.EqualFold(left.Columns[i].Name, right.Columns[i].Name) || left.Columns[i].Type != TypeUnknown && right.Columns[i].Type != TypeUnknown && left.Columns[i].Type != right.Columns[i].Type {
			return false
		}
	}
	return true
}

func spanOfExpressions(expressions []kql.Expression) kql.Span {
	if len(expressions) == 0 {
		return kql.Span{}
	}
	first, last := expressions[0].NodeSpan(), expressions[len(expressions)-1].NodeSpan()
	return kql.Span{Offset: first.Offset, Length: last.Offset + last.Length - first.Offset, Line: first.Line, Column: first.Column}
}

func nameSet(names []string) map[string]struct{} {
	result := make(map[string]struct{}, len(names))
	for _, name := range names {
		result[strings.ToLower(name)] = struct{}{}
	}
	return result
}

func nameSetFromColumns(columns []Column) map[string]struct{} {
	result := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		result[strings.ToLower(column.Name)] = struct{}{}
	}
	return result
}

func andExpression(left, right sqlast.Expr) sqlast.Expr {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return &sqlast.Binary{Left: left, Operator: "AND", Right: right}
}

func isAlphaNumeric(value string) bool {
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return false
		}
	}
	return value != ""
}
