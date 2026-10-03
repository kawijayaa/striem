package ksql

import (
	"strings"

	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

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
	if operator.Kind == "invoke" {
		return s.invoke(input, operator)
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
		items, schema := s.projectItems(spec.Expressions, false, input.Schema, false)
		return Relation{Query: &sqlast.Select{From: from, Projections: items}, Schema: schema}
	case "extend":
		spec := operator.Body.(kql.ExpressionSpec)
		return s.extend(from, input.Schema, spec.Expressions, operator)
	case "project-away", "project-keep", "project-rename", "project-reorder":
		return s.projectionOperator(from, input.Schema, operator)
	case "summarize":
		spec := operator.Body.(kql.SummarizeSpec)
		groups := s.expressions(spec.By)
		groupItems, groupSchema := s.projectItems(spec.By, true, input.Schema, false)
		if !s.checkNonDynamicColumns(groupSchema.Columns, spanOfExpressions(spec.By), feature) {
			return Relation{}
		}
		aggregateItems, aggregateSchema := s.projectItems(spec.Aggregates, true, input.Schema, false)
		groupSchema.Columns = append(groupSchema.Columns, aggregateSchema.Columns...)
		if !s.checkProjectionWidth(len(groupSchema.Columns), operator.Span, feature) {
			return Relation{}
		}
		s.validateUnique(groupSchema.Columns, operator.Span, feature)
		return Relation{Query: &sqlast.Select{From: from, Projections: append(groupItems, aggregateItems...), GroupBy: groups}, Schema: groupSchema}
	case "distinct":
		spec := operator.Body.(kql.ExpressionSpec)
		items, schema := s.projectItems(spec.Expressions, false, input.Schema, false)
		if !s.checkNonDynamicColumns(schema.Columns, spanOfExpressions(spec.Expressions), feature) {
			return Relation{}
		}
		return Relation{Query: &sqlast.Select{Distinct: true, From: from, Projections: items}, Schema: schema}
	case "getschema":
		return s.getSchema(input.Schema, operator)
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
		name, isName := spec.Column.(*kql.NameExpression)
		if !isName {
			s.bindError("KQLB0304", spec.Column.NodeSpan(), feature, "sample-distinct requires a column reference")
			return Relation{}
		}
		logicalName := unquoteKQL(name.Name)
		_, scalarLet := s.scalarLets[strings.ToLower(logicalName)]
		if !s.hasInputName(logicalName) && (!input.Schema.Unknown || scalarLet) {
			s.bindError("KQLB0304", spec.Column.NodeSpan(), feature, "sample-distinct requires a column reference")
			return Relation{}
		}
		column := s.expression(spec.Column)
		if !s.checkNonDynamicColumns([]Column{{Name: column.Name, Type: column.Type}}, spec.Column.NodeSpan(), feature) {
			return Relation{}
		}
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

func supportsRandom(target dialect.Dialect) bool {
	capability, ok := target.(interface{ Random() (string, bool) })
	if !ok {
		return false
	}
	_, ok = capability.Random()
	return ok
}
