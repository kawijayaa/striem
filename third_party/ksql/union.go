package ksql

import (
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

// union aligns each leg by (column name, scalar type), never by position.
func (s *compileState) union(input Relation, operator kql.Operator) Relation {
	spec := operator.Body.(kql.UnionSpec)
	if spec.Kind != "inner" && spec.Kind != "outer" {
		s.error("KQLL0321", operator.Span, "operator.union", "unknown union kind "+spec.Kind)
		return Relation{}
	}
	var legs []Relation
	if input.Query != nil {
		legs = append(legs, input)
	}
	for _, pipeline := range spec.Inputs {
		leg := s.pipeline(pipeline)
		if leg.Query == nil {
			return Relation{}
		}
		legs = append(legs, leg)
	}
	type key struct {
		name string
		typ  ScalarType
	}
	var keys []key
	counts := map[key]int{}
	types := map[string]map[ScalarType]bool{}
	for _, leg := range legs {
		if leg.Schema.Unknown {
			s.bindError("KQLB0320", operator.Span, "operator.union", "union requires bound input schemas to align columns by name and type")
			return Relation{}
		}
		seen := map[string]bool{}
		for _, col := range leg.Schema.Columns {
			// SQL targets cannot reliably represent names differing only in case.
			folded := strings.ToLower(col.Name)
			if seen[folded] {
				s.bindError("KQLB0321", operator.Span, "operator.union", "ambiguous union input column "+col.Name)
				return Relation{}
			}
			seen[folded] = true
			k := key{col.Name, col.Type}
			if counts[k] == 0 {
				keys = append(keys, k)
			}
			counts[k]++
			if types[col.Name] == nil {
				types[col.Name] = map[ScalarType]bool{}
			}
			types[col.Name][col.Type] = true
		}
	}
	selected := make([]key, 0, len(keys))
	for _, k := range keys {
		if spec.Kind == "outer" || counts[k] == len(legs) {
			selected = append(selected, k)
		}
	}
	if maximum := s.compiler.limits.MaxProjectionItems; maximum > 0 && len(selected) > maximum {
		s.bindError("KQLB0344", operator.Span, "operator.union", fmt.Sprintf("union output exceeds MaxProjectionItems (%d)", maximum))
		return Relation{}
	}
	if len(selected) == 0 {
		s.error("KQLL0323", operator.Span, "operator.union", "zero-column union results are not yet supported")
		return Relation{}
	}
	// Reserve unchanged names first so a synthesized x_long becomes x_long1
	// when an actual x_long column also exists, independent of leg ordering.
	used := map[string]bool{}
	for _, k := range selected {
		if len(types[k.name]) == 1 {
			folded := strings.ToLower(k.name)
			if used[folded] {
				s.bindError("KQLB0321", operator.Span, "operator.union", "case-distinct union columns cannot be represented by this SQL backend")
				return Relation{}
			}
			used[folded] = true
		}
	}
	schema := Schema{}
	for _, k := range selected {
		name := k.name
		if len(types[k.name]) > 1 {
			base := k.name + "_" + string(k.typ)
			name = base
			for suffix := 1; used[strings.ToLower(name)]; suffix++ {
				name = fmt.Sprintf("%s%d", base, suffix)
			}
			used[strings.ToLower(name)] = true
		}
		schema.Columns = append(schema.Columns, Column{Name: name, Type: k.typ})
	}
	queries := make([]sqlast.Query, 0, len(legs))
	for _, leg := range legs {
		alias := s.nextAlias()
		available := map[key]bool{}
		for _, col := range leg.Schema.Columns {
			available[key{col.Name, col.Type}] = true
		}
		items := make([]sqlast.SelectItem, 0, len(selected))
		for i, k := range selected {
			var value sqlast.Expr = &sqlast.Literal{Kind: sqlast.NullLiteral}
			if available[k] {
				value = &sqlast.Identifier{Parts: []string{alias, k.name}}
			}
			items = append(items, sqlast.SelectItem{Expr: value, Alias: schema.Columns[i].Name})
		}
		queries = append(queries, &sqlast.Select{From: &sqlast.Subquery{Query: leg.Query, Alias: alias}, Projections: items})
	}
	return Relation{Query: &sqlast.Union{All: true, Queries: queries}, Schema: schema}
}
