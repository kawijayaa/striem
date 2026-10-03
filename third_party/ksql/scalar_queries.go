package ksql

import (
	"fmt"
	"maps"
	"strings"

	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

type scalarBinding struct {
	reference *sqlast.ScalarReference
	query     sqlast.Query
}

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
	input, qualifier, qualifiers := s.input, s.qualifier, s.qualifiers
	scalars, captures, blocked := s.scalarLets, s.capturedRows, s.blockedRows
	s.input, s.qualifier, s.qualifiers, s.capturedRows = Schema{}, "", nil, nil
	s.scalarLets = maps.Clone(scalars)
	s.blockedRows = maps.Clone(blocked)
	if s.blockedRows == nil {
		s.blockedRows = map[string]bool{}
	}
	for _, column := range input.Columns {
		s.blockedRows[strings.ToLower(column.Name)] = true
	}
	for name := range qualifiers {
		s.blockedRows[strings.ToLower(name)] = true
	}
	if !s.functionValidation {
		for name, value := range s.scalarLets {
			if len(value.rows) != 0 {
				s.blockedRows[name] = true
			}
		}
	}
	defer func() {
		s.input, s.qualifier, s.qualifiers = input, qualifier, qualifiers
		s.scalarLets, s.capturedRows, s.blockedRows = scalars, captures, blocked
	}()
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
	cost := functionExpressionSize(query)
	if len(s.scalarBindings) >= 1024 || cost > maxFunctionSQLWork-s.scalarSQLWork {
		s.bindError("KQLB0523", call.Span, "function.toscalar", "materialized scalar binding work limit exceeded")
		return boundExpr{}
	}
	s.scalarSQLWork += cost
	reference := &sqlast.ScalarReference{Name: fmt.Sprintf("__ksql_scalar_%d", len(s.scalarBindings))}
	s.scalarBindings = append(s.scalarBindings, scalarBinding{reference: reference, query: query})
	return boundExpr{SQL: reference, Type: kind}
}

// Retain reachable bindings only: declaration-validation placeholders and
// unused lets must not execute. Each dependency precedes its consumer.
func (s *compileState) materializeScalars(query sqlast.Query) sqlast.Query {
	if query == nil || len(s.scalarBindings) == 0 {
		return query
	}
	byReference := make(map[*sqlast.ScalarReference]sqlast.Query, len(s.scalarBindings))
	for _, binding := range s.scalarBindings {
		byReference[binding.reference] = binding.query
	}
	reachable := map[*sqlast.ScalarReference]bool{}
	physicalNames := map[string]bool{}
	var collect func(any)
	collect = func(root any) {
		sqlast.Walk(root, func(node any) bool {
			switch value := node.(type) {
			case *sqlast.Table:
				if len(value.Parts) == 1 {
					physicalNames[strings.ToLower(value.Parts[0])] = true
				}
			case *sqlast.ScalarReference:
				if !reachable[value] {
					reachable[value] = true
					collect(byReference[value])
				}
			}
			return true
		})
	}
	collect(query)
	var bindings []sqlast.Binding
	candidate := 0
	for _, binding := range s.scalarBindings {
		if !reachable[binding.reference] {
			continue
		}
		var name string
		for {
			name = fmt.Sprintf("__ksql_scalar_%d", candidate)
			candidate++
			if !physicalNames[strings.ToLower(name)] {
				break
			}
		}
		physicalNames[strings.ToLower(name)] = true
		binding.reference.Name = name
		bindings = append(bindings, sqlast.Binding{Name: name, Query: binding.query})
	}
	if len(bindings) == 0 {
		return query
	}
	return &sqlast.With{Bindings: bindings, Query: query}
}
