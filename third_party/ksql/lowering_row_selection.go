package ksql

import (
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

// Only built-in row selectors take this path; explicit application mappings
// and query functions keep their normal binding precedence.
func (s *compileState) rowSelectionCall(expression kql.Expression) (*kql.CallExpression, string, string) {
	alias := ""
	if named, ok := expression.(*kql.NamedExpression); ok {
		alias = unquoteKQL(named.Name)
		expression = named.Value
	}
	if tuple, ok := expression.(*kql.CompoundNamedExpression); ok {
		if len(tuple.Names) > 0 {
			alias = unquoteKQL(tuple.Names[0])
		}
		expression = tuple.Value
	}
	call, ok := expression.(*kql.CallExpression)
	if !ok {
		return nil, "", ""
	}
	name, ok := call.Function.(*kql.NameExpression)
	if !ok {
		return nil, "", ""
	}
	function := strings.ToLower(unquoteKQL(name.Name))
	if function != "arg_max" && function != "arg_min" && function != "argmax" && function != "argmin" {
		return nil, "", ""
	}
	if s.functions[function] != nil || s.compiler.functionRules[function] != nil || s.compiler.typedFunctionRules[function] != nil {
		return nil, "", ""
	}
	return call, function, alias
}

func (s *compileState) hasRowSelection(expressions []kql.Expression) bool {
	for _, expression := range expressions {
		if call, _, _ := s.rowSelectionCall(expression); call != nil {
			return true
		}
	}
	return false
}

type rowSelection struct {
	primary   string
	rank      string
	direction string
}

func (s *compileState) summarizeRows(input Relation, spec kql.SummarizeSpec, operator kql.Operator) Relation {
	feature := "operator.summarize"
	if input.Schema.Unknown {
		s.bindError("KQLB0350", operator.Span, feature, "row-selection aggregates require a bound input schema")
		return Relation{}
	}
	if _, ok := s.compiler.dialect.(dialect.RankedSelection); !ok {
		s.error("KQLL0350", operator.Span, feature, "dialect has no guaranteed ranked row selection")
		return Relation{}
	}
	if !s.validateUnique(input.Schema.Columns, operator.Span, feature) {
		return Relation{}
	}
	before := len(s.diagnostics)
	groupItems, groupSchema := s.projectItems(spec.By, true, input.Schema, false)
	if !s.checkNonDynamicColumns(groupSchema.Columns, spanOfExpressions(spec.By), feature) {
		return Relation{}
	}
	output := groupSchema.Clone()
	outputNames := nameSet(inputColumnNames(output))
	nextSuffix := map[string]int{}
	usedNames := nameSet(inputColumnNames(input.Schema))
	nextInternal := 0
	internalName := func() string {
		for {
			name := fmt.Sprintf("__ksql_selection_%d", nextInternal)
			nextInternal++
			if _, used := usedNames[strings.ToLower(name)]; !used {
				usedNames[strings.ToLower(name)] = struct{}{}
				return name
			}
		}
	}
	preparedItems := make([]sqlast.SelectItem, 0, len(input.Schema.Columns)+len(groupItems))
	preparedSchema := input.Schema.Clone()
	for _, column := range input.Schema.Columns {
		preparedItems = append(preparedItems, sqlast.SelectItem{Expr: s.columnIdentifier(column.Name), Alias: column.Name})
	}
	prepare := func(value boundExpr) string {
		name := internalName()
		preparedItems = append(preparedItems, sqlast.SelectItem{Expr: value.SQL, Alias: name})
		preparedSchema.Columns = append(preparedSchema.Columns, Column{Name: name, Type: value.Type})
		return name
	}
	groups := make([]sqlast.Expr, len(groupItems))
	projections := make([]sqlast.SelectItem, 0, len(groupItems)+len(spec.Aggregates))
	byColumns := map[string]bool{}
	for i, item := range groupItems {
		if id, ok := item.Expr.(*sqlast.Identifier); ok && len(id.Parts) == 1 {
			byColumns[strings.ToLower(id.Parts[0])] = true
		}
		name := prepare(boundExpr{SQL: item.Expr, Type: groupSchema.Columns[i].Type})
		groups[i] = &sqlast.Identifier{Parts: []string{name}}
		projections = append(projections, sqlast.SelectItem{Expr: groups[i], Alias: groupSchema.Columns[i].Name})
	}
	// Summarize adds implicit tuple columns with unique names. Explicit output
	// aliases remain declarations and duplicate declarations are diagnosed.
	implicitName := func(name string) string {
		key := strings.ToLower(name)
		candidate := name
		for suffix := nextSuffix[key]; ; suffix++ {
			if suffix > 0 {
				candidate = fmt.Sprintf("%s%d", name, suffix)
			}
			if _, used := outputNames[strings.ToLower(candidate)]; !used {
				nextSuffix[key] = suffix + 1
				return candidate
			}
		}
	}
	selections := []rowSelection{}
	for _, expression := range spec.Aggregates {
		call, function, alias := s.rowSelectionCall(expression)
		if call == nil {
			items, schema := s.projectItems([]kql.Expression{expression}, true, input.Schema, false)
			projections = append(projections, items...)
			output.Columns = append(output.Columns, schema.Columns...)
			for _, column := range schema.Columns {
				outputNames[strings.ToLower(column.Name)] = struct{}{}
			}
			continue
		}
		aliases := []string{}
		if tuple, ok := expression.(*kql.CompoundNamedExpression); ok {
			aliases = tuple.Names
		} else if alias != "" {
			aliases = []string{alias}
		}
		startColumn := len(output.Columns)
		assignName := func(name string) string {
			index := len(output.Columns) - startColumn
			if index < len(aliases) {
				name = unquoteKQL(aliases[index])
			} else {
				name = implicitName(name)
			}
			outputNames[strings.ToLower(name)] = struct{}{}
			return name
		}
		s.use("aggregate."+function, "equivalent", call.Span)
		if len(call.Args) == 0 || len(call.Args) > 128 {
			s.bindError("KQLB0351", call.Span, "aggregate."+function, "row-selection aggregate requires 1 to 128 scalar or wildcard arguments")
			continue
		}
		if _, star := call.Args[0].(*kql.StarExpression); star {
			s.bindError("KQLB0351", call.Span, "aggregate."+function, "row-selection ordering expression must be scalar")
			continue
		}
		rank := internalName()
		primary := boundExpr{}
		selected := map[string]bool{}
		anyStar := false
		for _, arg := range call.Args {
			if _, star := arg.(*kql.StarExpression); star {
				anyStar = true
			}
		}
		prefix := "max"
		direction := "DESC"
		if function == "arg_min" || function == "argmin" {
			prefix, direction = "min", "ASC"
		}
		legacy := function == "argmax" || function == "argmin"
		primaryName := ""
		primaryColumnName := ""
		for i, arg := range call.Args {
			if _, star := arg.(*kql.StarExpression); star {
				for _, column := range input.Schema.Columns {
					key := strings.ToLower(column.Name)
					if selected[key] || byColumns[key] {
						continue
					}
					selected[key] = true
					name := column.Name
					if legacy {
						name = prefix + "_" + primaryName + "_" + name
					}
					name = assignName(name)
					projections = append(projections, sqlast.SelectItem{Expr: &sqlast.RankedValue{Value: &sqlast.Identifier{Parts: []string{column.Name}}, Rank: &sqlast.Identifier{Parts: []string{rank}}}, Alias: name})
					output.Columns = append(output.Columns, Column{Name: name, Type: column.Type})
					if !s.checkProjectionWidth(len(output.Columns), arg.NodeSpan(), feature) {
						return Relation{}
					}
				}
				continue
			}
			expr := arg
			explicit := ""
			if named, ok := arg.(*kql.NamedExpression); ok {
				explicit = unquoteKQL(named.Name)
				expr = named.Value
			}
			value := s.expression(expr)
			if value.SQL == nil {
				continue
			}
			columnName := ""
			if id, ok := value.SQL.(*sqlast.Identifier); ok && len(id.Parts) == 1 {
				if column, _, found, _ := input.Schema.Lookup(id.Parts[0]); found {
					columnName = column.Name
				}
			}
			if i > 0 && anyStar && columnName != "" && (selected[strings.ToLower(columnName)] || byColumns[strings.ToLower(columnName)]) {
				continue
			}
			if columnName != "" {
				selected[strings.ToLower(columnName)] = true
			}
			name := inferredName(expr, value.Name)
			if explicit != "" {
				name = explicit
			}
			if i == 0 {
				primary = value
				if value.Type == TypeDynamic || value.Type == TypeUnknown {
					s.bindError("KQLB0352", arg.NodeSpan(), "aggregate."+function, "row-selection ordering expression requires a known orderable scalar type")
					continue
				}
				primaryName = name
				if name == "" {
					name = prefix + "_"
				}
			} else if name == "" {
				if legacy {
					name = fmt.Sprintf("arg%d", i)
				} else {
					name = prefix + "_" + primaryName + fmt.Sprintf("_arg%d", i)
				}
			}
			if legacy {
				if i == 0 {
					name = prefix + "_" + primaryName
				} else {
					name = prefix + "_" + primaryName + "_" + name
				}
			}
			name = assignName(name)
			preparedName := prepare(value)
			if i == 0 {
				primaryColumnName = preparedName
			}
			projections = append(projections, sqlast.SelectItem{Expr: &sqlast.RankedValue{Value: &sqlast.Identifier{Parts: []string{preparedName}}, Rank: &sqlast.Identifier{Parts: []string{rank}}}, Alias: name})
			output.Columns = append(output.Columns, Column{Name: name, Type: value.Type})
			if !s.checkProjectionWidth(len(output.Columns), arg.NodeSpan(), feature) {
				return Relation{}
			}
		}
		if len(aliases) > len(output.Columns)-startColumn {
			s.bindError("KQLB0353", expression.NodeSpan(), "aggregate."+function, "tuple assignment has more names than aggregate result columns")
		}
		if primary.SQL != nil && primaryColumnName != "" {
			selections = append(selections, rowSelection{primary: primaryColumnName, rank: rank, direction: direction})
		}
	}
	if !s.checkProjectionWidth(len(output.Columns), operator.Span, feature) || !s.validateUnique(output.Columns, operator.Span, feature) || len(s.diagnostics) != before {
		return Relation{}
	}
	// Precompute keys/payloads once. In particular a nondeterministic primary
	// expression must have the same value when ranking and returning it.
	input = s.materializeRelation(input, operator.Span, feature)
	if input.Query == nil {
		return Relation{}
	}
	prepared := s.materializeRelation(Relation{Query: &sqlast.Select{From: &sqlast.Subquery{Query: input.Query, Alias: s.nextAlias()}, Projections: preparedItems}, Schema: preparedSchema, rowDependencies: input.rowDependencies}, operator.Span, feature)
	if prepared.Query == nil {
		return Relation{}
	}
	rankedItems := []sqlast.SelectItem{{Expr: &sqlast.Star{}}}
	for _, selection := range selections {
		primary := &sqlast.Identifier{Parts: []string{selection.primary}}
		nullRank := &sqlast.Case{Branches: []sqlast.When{{Condition: &sqlast.Binary{Left: primary, Operator: "IS", Right: &sqlast.Literal{Kind: sqlast.NullLiteral}}, Result: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "1"}}}, Else: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "0"}}
		rankedItems = append(rankedItems, sqlast.SelectItem{Expr: &sqlast.Window{Expr: &sqlast.Call{Name: "row_number"}, PartitionBy: groups, OrderBy: []sqlast.Order{{Expr: nullRank, Direction: "ASC"}, {Expr: primary, Direction: selection.direction}}}, Alias: selection.rank})
	}
	ranked := &sqlast.Select{From: &sqlast.Subquery{Query: prepared.Query, Alias: s.nextAlias()}, Projections: rankedItems}
	return Relation{Query: &sqlast.Select{From: &sqlast.Subquery{Query: ranked, Alias: s.nextAlias()}, Projections: projections, GroupBy: groups}, Schema: output}
}

func inputColumnNames(schema Schema) []string {
	names := make([]string, len(schema.Columns))
	for i, column := range schema.Columns {
		names[i] = column.Name
	}
	return names
}
