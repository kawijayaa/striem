package ksql

import (
	"maps"

	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

func mergeRowDependencies(left, right map[int]struct{}) map[int]struct{} {
	result := maps.Clone(left)
	if len(right) == 0 {
		return result
	}
	if result == nil {
		result = make(map[int]struct{}, len(right))
	}
	for row := range right {
		result[row] = struct{}{}
	}
	return result
}
func (s *compileState) captureRows(rows map[int]struct{}) {
	s.capturedRows = mergeRowDependencies(s.capturedRows, rows)
}
func (s *compileState) rowIdentifier(parts []string) sqlast.Expr {
	identifier := &sqlast.Identifier{Parts: parts}
	if s.rowID != 0 {
		if s.rowOrigins == nil {
			s.rowOrigins = map[*sqlast.Identifier]int{}
		}
		s.rowOrigins[identifier] = s.rowID
	}
	return identifier
}
func (s *compileState) expressionRows(expression sqlast.Expr) map[int]struct{} {
	var rows map[int]struct{}
	sqlast.Walk(expression, func(node any) bool {
		// A subquery has its own row scope; captured scalar references are recorded
		// while binding its expressions and propagated through Relation instead.
		if _, query := node.(sqlast.Query); query {
			return false
		}
		if identifier, ok := node.(*sqlast.Identifier); ok {
			if row := s.rowOrigins[identifier]; row != 0 {
				if rows == nil {
					rows = map[int]struct{}{}
				}
				rows[row] = struct{}{}
			}
		}
		return true
	})
	return rows
}

func (s *compileState) toScalar(call *kql.CallExpression) boundExpr {
	s.use("function.toscalar", "equivalent", call.Span)
	if len(call.Args) != 1 {
		return s.arity(call, "toscalar", 1)
	}
	capability, supported := s.compiler.dialect.(dialect.Materialization)
	if !supported || !capability.SupportsMaterializedBindings() {
		s.error("KQLL0520", call.Span, "function.toscalar", "toscalar requires guaranteed materialized bindings on this dialect")
		return boundExpr{}
	}
	if _, named := call.Args[0].(*kql.NamedExpression); named {
		s.bindError("KQLB0520", call.Span, "function.toscalar", "toscalar requires one unnamed scalar or tabular expression")
		return boundExpr{}
	}
	restore := s.withoutCallerRows("function.toscalar")
	defer restore()
	var query sqlast.Query
	var kind ScalarType
	if relation, table := s.functionTableResult(call.Args[0]); table {
		if relation.Query == nil {
			return boundExpr{}
		}
		if len(relation.rowDependencies) != 0 && !s.functionValidation {
			s.bindError("KQLB0521", call.Span, "function.toscalar", "toscalar cannot depend on a caller's row-context scope")
			return boundExpr{}
		}
		if (relation.Schema.Unknown || len(relation.Schema.Columns) == 0) && s.functionValidation {
			// Wildcard table parameters have no first-column type until a call
			// supplies their actual schema. This placeholder is never rendered.
			relation = Relation{Query: &sqlast.Select{Projections: []sqlast.SelectItem{{Expr: &sqlast.Literal{Kind: sqlast.NullLiteral}, Alias: "value"}}}, Schema: Schema{Columns: []Column{{Name: "value", Type: TypeUnknown}}}}
		}
		if relation.Schema.Unknown || len(relation.Schema.Columns) == 0 {
			s.bindError("KQLB0522", call.Span, "function.toscalar", "toscalar table requires a known first output column")
			return boundExpr{}
		}
		column, alias := relation.Schema.Columns[0], s.nextAlias()
		kind = column.Type
		query = &sqlast.Select{
			Projections: []sqlast.SelectItem{{Expr: &sqlast.Identifier{Parts: []string{alias, column.Name}}, Alias: "value"}},
			From:        &sqlast.Subquery{Query: relation.Query, Alias: alias},
			Limit:       &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "1"},
		}
	} else {
		value := s.expression(call.Args[0])
		if value.SQL == nil {
			return boundExpr{}
		}
		if (len(value.rows) != 0 || len(s.capturedRows) != 0) && !s.functionValidation {
			s.bindError("KQLB0521", call.Span, "function.toscalar", "toscalar cannot depend on a caller's row-context scope")
			return boundExpr{}
		}
		kind = value.Type
		query = &sqlast.Select{Projections: []sqlast.SelectItem{{Expr: value.SQL, Alias: "value"}}}
	}
	reference := &sqlast.ScalarReference{}
	if !s.addQueryBinding(reference, query, call.Span, "function.toscalar") {
		return boundExpr{}
	}
	return boundExpr{SQL: reference, Type: kind}
}
