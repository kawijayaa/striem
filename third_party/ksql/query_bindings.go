package ksql

import (
	"fmt"
	"maps"
	"strings"

	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

type queryBinding struct {
	reference any
	query     sqlast.Query
}

// Retain reachable bindings only: declaration-validation placeholders and
// unused lets must not execute. Each dependency precedes its consumer.
func (s *compileState) materializeQueries(query sqlast.Query) sqlast.Query {
	if query == nil || len(s.queryBindings) == 0 {
		return query
	}
	byReference := make(map[any]sqlast.Query, len(s.queryBindings))
	for _, binding := range s.queryBindings {
		byReference[binding.reference] = binding.query
	}
	reachable := map[any]bool{}
	physicalNames := map[string]bool{}
	var collect func(any)
	collect = func(root any) {
		sqlast.Walk(root, func(node any) bool {
			switch value := node.(type) {
			case *sqlast.Table:
				if len(value.Parts) == 1 {
					physicalNames[strings.ToLower(value.Parts[0])] = true
				}
			case *sqlast.ScalarReference, *sqlast.MaterializedReference:
				if !reachable[node] {
					reachable[node] = true
					collect(byReference[node])
				}
			}
			return true
		})
	}
	collect(query)
	var bindings []sqlast.Binding
	candidate := 0
	for _, binding := range s.queryBindings {
		if !reachable[binding.reference] {
			continue
		}
		prefix := "__ksql_scalar_"
		if _, table := binding.reference.(*sqlast.MaterializedReference); table {
			prefix = "__ksql_table_"
		}
		var name string
		for {
			name = fmt.Sprintf("%s%d", prefix, candidate)
			candidate++
			if !physicalNames[strings.ToLower(name)] {
				break
			}
		}
		physicalNames[strings.ToLower(name)] = true
		switch reference := binding.reference.(type) {
		case *sqlast.ScalarReference:
			reference.Name = name
		case *sqlast.MaterializedReference:
			reference.Name = name
		}
		bindings = append(bindings, sqlast.Binding{Name: name, Query: binding.query})
	}
	if len(bindings) == 0 {
		return query
	}
	return &sqlast.With{Bindings: bindings, Query: query}
}

// Detach caller rows before binding a query-wide cached expression.
func (s *compileState) withoutCallerRows(feature string) func() {
	input, qualifier, qualifiers := s.input, s.qualifier, s.qualifiers
	scalars, captures, blocked := s.scalarLets, s.capturedRows, s.blockedRows
	previousFeature := s.blockedFeature
	s.blockedFeature = feature
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
	return func() {
		s.input, s.qualifier, s.qualifiers = input, qualifier, qualifiers
		s.scalarLets, s.capturedRows, s.blockedRows = scalars, captures, blocked
		s.blockedFeature = previousFeature
	}
}

func (s *compileState) addQueryBinding(reference any, query sqlast.Query, span kql.Span, feature string) bool {
	cost := functionExpressionSize(query)
	if len(s.queryBindings) >= 1024 || cost > maxFunctionSQLWork-s.materializationSQLWork {
		s.bindError("KQLB0523", span, feature, "materialized query binding work limit exceeded")
		return false
	}
	s.materializationSQLWork += cost
	s.queryBindings = append(s.queryBindings, queryBinding{reference: reference, query: query})
	return true
}
