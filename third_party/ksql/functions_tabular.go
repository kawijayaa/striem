package ksql

import (
	"strings"

	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

func functionScalarType(name string) (ScalarType, bool) {
	switch strings.ToLower(name) {
	case "bool", "int", "long", "real", "double", "string", "datetime", "dynamic":
		return parseScalarType(name), true
	}
	return TypeUnknown, false
}

func (s *compileState) functionParameterSchema(parameter kql.FunctionParameter) (Schema, bool) {
	schema := Schema{}
	for _, column := range parameter.Columns {
		kind, valid := functionScalarType(column.Type)
		if !valid {
			s.bindError("KQLB0123", parameter.Span, "statement.let", "unsupported tabular parameter column type: "+column.Type)
			return Schema{}, false
		}
		name := unquoteKQL(column.Name)
		if _, _, found, _ := schema.Lookup(name); found {
			s.bindError("KQLB0123", parameter.Span, "statement.let", "duplicate tabular parameter column: "+name)
			return Schema{}, false
		}
		schema.Columns = append(schema.Columns, Column{Name: name, Type: kind})
	}
	return schema, true
}

func functionTablePlaceholder(schema Schema) Relation {
	items := make([]sqlast.SelectItem, len(schema.Columns))
	for i, column := range schema.Columns {
		items[i] = sqlast.SelectItem{Expr: &sqlast.Literal{Kind: sqlast.NullLiteral}, Alias: column.Name}
	}
	if len(items) == 0 {
		items = []sqlast.SelectItem{{Expr: &sqlast.Literal{Kind: sqlast.NullLiteral}, Alias: "_function_validation"}}
	}
	// Wildcard parameters expose no named columns during declaration checking.
	// The actual schema is used at call sites to bind star projections.
	return Relation{Query: &sqlast.Select{Projections: items}, Schema: schema.Clone()}
}

func (s *compileState) functionTableArgument(input Relation, required Schema, wildcard bool, span kql.Span) (Relation, bool) {
	if wildcard {
		return input, true
	}
	if input.Schema.Unknown {
		s.bindError("KQLB0124", span, "statement.let", "fixed-schema function arguments require a known table schema")
		return Relation{}, false
	}
	for _, column := range required.Columns {
		actual, _, found, ambiguous := input.Schema.Lookup(column.Name)
		if !found || ambiguous || actual.Type != column.Type {
			s.bindError("KQLB0124", span, "statement.let", "tabular argument requires column "+column.Name+":"+string(column.Type))
			return Relation{}, false
		}
	}
	// Validate the declared contract, retaining the argument's actual schema and
	// columns when expanding the body, as Microsoft's call-site binder does.
	return input, true
}

func (s *compileState) functionTableResult(expression kql.Expression) (Relation, bool) {
	switch value := expression.(type) {
	case *kql.PipelineExpression:
		return s.pipeline(value.Pipeline), true
	case *kql.CallExpression:
		if name, ok := value.Function.(*kql.NameExpression); ok {
			if function := s.functions[strings.ToLower(unquoteKQL(name.Name))]; function != nil && function.tabular {
				return s.callFunctionValue(function, value, nil).table, true
			}
		}
	case *kql.NameExpression:
		name := unquoteKQL(value.Name)
		key := strings.ToLower(name)
		if _, scalar := s.scalarLets[key]; scalar {
			return Relation{}, false
		}
		if relation, found := s.tabularLets[key]; found {
			s.captureRows(relation.rowDependencies)
			return relation, true
		}
		if function := s.functions[key]; function != nil && function.tabular {
			return s.callFunctionValue(function, &kql.CallExpression{Function: value, Span: value.Span}, nil).table, true
		}
		if s.compiler.catalog != nil {
			if _, found := s.compiler.catalog.ResolveTable(name); found {
				return s.source(kql.Source{Kind: "table", Name: name, Expr: value, Span: value.Span}), true
			}
		}
	}
	return Relation{}, false
}

func (s *compileState) functionTableExpression(expression kql.Expression) (Relation, bool) {
	if relation, table := s.functionTableResult(expression); table {
		return relation, true
	}
	return s.tabularLetValue(expression)
}

func (s *compileState) invoke(input Relation, operator kql.Operator) Relation {
	body, valid := operator.Body.(kql.ExpressionSpec)
	if !valid || len(body.Expressions) != 1 || body.Expressions[0] == nil {
		s.bindError("KQLB0121", operator.Span, "operator.invoke", "invoke requires a function call")
		return Relation{}
	}
	expression := body.Expressions[0]
	call, ok := expression.(*kql.CallExpression)
	if !ok {
		s.bindError("KQLB0121", operator.Span, "operator.invoke", "invoke requires a function call")
		return Relation{}
	}
	name, ok := call.Function.(*kql.NameExpression)
	if !ok {
		s.bindError("KQLB0121", operator.Span, "operator.invoke", "invoke requires a named query function")
		return Relation{}
	}
	function := s.functions[strings.ToLower(unquoteKQL(name.Name))]
	if function == nil || !function.tabular {
		s.bindError("KQLB0121", operator.Span, "operator.invoke", "invoke requires a function returning a table")
		return Relation{}
	}
	return s.callFunctionValue(function, call, &input).table
}
