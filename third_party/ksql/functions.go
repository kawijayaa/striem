package ksql

import (
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

const maxFunctionDepth = 64
const maxFunctionWork = 65536
const maxFunctionSQLWork = 4 << 20

type boundFunction struct {
	name       string
	definition *kql.FunctionDefinition
	parameters []Column
	defaults   []boundExpr
	scalars    map[string]boundExpr
	tables     map[string]Relation
	functions  map[string]*boundFunction
}

func (s *compileState) bindLet(node *kql.LetStatement, locals map[string]struct{}) {
	key := strings.ToLower(node.Name)
	_, scalarExists := s.scalarLets[key]
	_, tableExists := s.tabularLets[key]
	_, functionExists := s.functions[key]
	duplicate := scalarExists || tableExists || functionExists
	if locals != nil {
		_, duplicate = locals[key]
	}
	if duplicate {
		s.bindError("KQLB0101", node.Span, "statement.let", "variable already declared: "+node.Name)
		return
	}
	if locals != nil {
		locals[key] = struct{}{}
	}
	input, qualifier, qualifiers := s.input, s.qualifier, s.qualifiers
	s.input, s.qualifier, s.qualifiers = Schema{}, "", nil
	defer func() { s.input, s.qualifier, s.qualifiers = input, qualifier, qualifiers }()
	s.use("statement.let", "equivalent", node.Span)
	if node.Function != nil {
		function := s.defineFunction(node)
		if function != nil {
			delete(s.scalarLets, key)
			delete(s.tabularLets, key)
			s.functions[key] = function
		}
		return
	}
	var table Relation
	var scalar boundExpr
	if node.Pipeline != nil {
		table = s.pipeline(node.Pipeline)
	} else if node.Value != nil {
		if relation, tabular := s.tabularLetValue(node.Value); tabular {
			table = relation
		} else {
			scalar = s.expression(node.Value)
		}
	}
	if table.Query != nil {
		delete(s.scalarLets, key)
		delete(s.functions, key)
		s.tabularLets[key] = table
	} else if scalar.SQL != nil {
		delete(s.tabularLets, key)
		delete(s.functions, key)
		s.scalarLets[key] = scalar
	}
}

func functionDefaultLiteral(expression kql.Expression) bool {
	switch value := expression.(type) {
	case *kql.LiteralExpression, *kql.DynamicExpression:
		return true
	case *kql.UnaryExpression:
		literal, ok := value.Operand.(*kql.LiteralExpression)
		return ok && literal.Kind == kql.NumberToken && (value.Operator == "-" || value.Operator == "+")
	case *kql.CallExpression:
		name, ok := value.Function.(*kql.NameExpression)
		if !ok || len(value.Args) != 1 {
			return false
		}
		switch strings.ToLower(name.Name) {
		case "datetime", "date", "timespan", "time", "int", "long", "real", "double", "bool":
			return functionDefaultLiteral(value.Args[0])
		}
	}
	return false
}

func functionArgument(value boundExpr, expected ScalarType) (boundExpr, bool) {
	if value.Type == expected || value.Type == TypeUnknown {
		value.Type = expected
		return value, true
	}
	if expected == TypeLong && value.Type == TypeInt {
		value.Type = expected
		return value, true
	}
	if expected == TypeReal && (value.Type == TypeLong || value.Type == TypeInt) {
		value.Type = expected
		value.SQL = &sqlast.Cast{Expr: value.SQL, Type: "DOUBLE PRECISION"}
		return value, true
	}
	return boundExpr{}, false
}

func (s *compileState) defineFunction(node *kql.LetStatement) *boundFunction {
	definition := node.Function
	if definition.Pipeline != nil {
		s.bindError("KQLB0110", definition.Span, "statement.let", "tabular function bodies require tabular function lowering")
		return nil
	}
	if len(definition.Parameters) > 64 {
		s.bindError("KQLB0111", definition.Span, "statement.let", "function parameter limit exceeded")
		return nil
	}
	function := &boundFunction{name: node.Name, definition: definition, scalars: maps.Clone(s.scalarLets), tables: maps.Clone(s.tabularLets), functions: maps.Clone(s.functions)}
	seen := map[string]struct{}{}
	optional := false
	placeholders := make([]boundExpr, len(definition.Parameters))
	for i, parameter := range definition.Parameters {
		name := unquoteKQL(parameter.Name)
		key := strings.ToLower(name)
		if _, duplicate := seen[key]; duplicate {
			s.bindError("KQLB0112", parameter.Span, "statement.let", "duplicate function parameter: "+name)
			return nil
		}
		seen[key] = struct{}{}
		var kind ScalarType
		switch strings.ToLower(parameter.Type) {
		case "bool", "int", "long", "real", "double", "string", "datetime", "dynamic":
			kind = parseScalarType(parameter.Type)
		default:
			s.bindError("KQLB0113", parameter.Span, "statement.let", "function parameter type is not supported: "+parameter.Type)
			return nil
		}
		function.parameters = append(function.parameters, Column{Name: name, Type: kind})
		var defaultValue boundExpr
		if parameter.Default != nil {
			optional = true
			if !functionDefaultLiteral(parameter.Default) {
				s.bindError("KQLB0114", parameter.Span, "statement.let", "function parameter default must be a scalar literal")
				return nil
			}
			var valid bool
			defaultValue, valid = functionArgument(s.expression(parameter.Default), kind)
			if !valid || defaultValue.SQL == nil {
				s.bindError("KQLB0115", parameter.Span, "statement.let", "function parameter default type does not match "+parameter.Type)
				return nil
			}
		} else if optional {
			s.bindError("KQLB0116", parameter.Span, "statement.let", "required parameters must precede optional parameters")
			return nil
		}
		function.defaults = append(function.defaults, defaultValue)
		placeholders[i] = boundExpr{SQL: &sqlast.Identifier{Parts: []string{name}}, Type: kind}
	}
	// Validate unused declarations as well as calls, without taking the caller's
	// row scope. Defaults/body parameters used only here are pruned before render.
	before := len(s.diagnostics)
	s.functionBody(function, placeholders, definition.Span)
	if len(s.diagnostics) != before {
		return nil
	}
	return function
}

func (s *compileState) callFunction(function *boundFunction, call *kql.CallExpression) boundExpr {
	if len(call.Args) > len(function.parameters) {
		s.bindError("KQLB0117", call.Span, "statement.let", "too many function arguments for "+function.name)
		return boundExpr{}
	}
	arguments := make([]boundExpr, len(function.parameters))
	supplied := make([]bool, len(arguments))
	named := false
	for position, expression := range call.Args {
		index := position
		if argument, ok := expression.(*kql.NamedExpression); ok {
			named = true
			index = -1
			for i, parameter := range function.parameters {
				if strings.EqualFold(unquoteKQL(argument.Name), parameter.Name) {
					index = i
					break
				}
			}
			expression = argument.Value
		} else if named {
			s.bindError("KQLB0117", expression.NodeSpan(), "statement.let", "positional arguments must precede named arguments")
			return boundExpr{}
		}
		if index < 0 || supplied[index] {
			s.bindError("KQLB0117", expression.NodeSpan(), "statement.let", "unknown or duplicate function argument")
			return boundExpr{}
		}
		value, valid := functionArgument(s.expression(expression), function.parameters[index].Type)
		if !valid || value.SQL == nil {
			s.bindError("KQLB0118", expression.NodeSpan(), "statement.let", fmt.Sprintf("argument %s requires %s", function.parameters[index].Name, function.parameters[index].Type))
			return boundExpr{}
		}
		arguments[index], supplied[index] = value, true
	}
	for i, provided := range supplied {
		if !provided {
			if function.defaults[i].SQL == nil {
				s.bindError("KQLB0117", call.Span, "statement.let", "missing function argument "+function.parameters[i].Name)
				return boundExpr{}
			}
			arguments[i] = function.defaults[i]
		}
	}
	return s.functionBody(function, arguments, call.Span)
}

func (s *compileState) functionBody(function *boundFunction, arguments []boundExpr, span kql.Span) boundExpr {
	if s.functionDepth >= maxFunctionDepth {
		s.bindError("KQLB0119", span, "statement.let", "function call nesting exceeds 64 levels")
		return boundExpr{}
	}
	scalars, tables, functions := s.scalarLets, s.tabularLets, s.functions
	input, qualifier, qualifiers := s.input, s.qualifier, s.qualifiers
	s.scalarLets, s.tabularLets, s.functions = maps.Clone(function.scalars), maps.Clone(function.tables), maps.Clone(function.functions)
	s.input, s.qualifier, s.qualifiers = Schema{}, "", nil
	s.functionDepth++
	defer func() {
		s.scalarLets, s.tabularLets, s.functions = scalars, tables, functions
		s.input, s.qualifier, s.qualifiers = input, qualifier, qualifiers
		s.functionDepth--
	}()
	locals := map[string]struct{}{}
	for i, parameter := range function.parameters {
		key := strings.ToLower(parameter.Name)
		s.scalarLets[key] = arguments[i]
		delete(s.tabularLets, key)
		delete(s.functions, key)
		locals[key] = struct{}{}
	}
	for _, declaration := range function.definition.Lets {
		s.bindLet(declaration, locals)
	}
	result := s.expression(function.definition.Result)
	if result.SQL != nil {
		cost := functionExpressionSize(result.SQL)
		if cost > maxFunctionSQLWork-s.functionSQLWork {
			s.bindError("KQLB0119", span, "statement.let", "expanded function SQL exceeds work limit")
			return boundExpr{}
		}
		s.functionSQLWork += cost
	}
	result.Name = ""
	return result
}

// Count expanded SQL, including repeated references to shared argument trees.
// Memoization makes the check linear in the compact DAG; a small declaration
// such as f(x) { x+x } must not expand exponentially during SQL rendering.
func functionExpressionSize(expression sqlast.Expr) int {
	type pointerKey struct {
		kind    reflect.Type
		address uintptr
	}
	memo := map[pointerKey]int{}
	visiting := map[pointerKey]bool{}
	var size func(reflect.Value) int
	size = func(value reflect.Value) int {
		if !value.IsValid() {
			return 0
		}
		switch value.Kind() {
		case reflect.Interface:
			if value.IsNil() {
				return 0
			}
			return size(value.Elem())
		case reflect.Pointer:
			if value.IsNil() {
				return 0
			}
			key := pointerKey{value.Type(), value.Pointer()}
			if visiting[key] {
				return maxFunctionSQLWork + 1
			}
			if result, found := memo[key]; found {
				return result
			}
			visiting[key] = true
			result := size(value.Elem())
			delete(visiting, key)
			memo[key] = result
			return result
		case reflect.String:
			if value.Len() > maxFunctionSQLWork/2 {
				return maxFunctionSQLWork + 1
			}
			return 16 + 2*value.Len()
		case reflect.Struct, reflect.Slice, reflect.Array:
			total, count := 16, 0
			var child func(int) reflect.Value
			if value.Kind() == reflect.Struct {
				count = value.NumField()
				child = value.Field
			} else {
				count = value.Len()
				child = value.Index
			}
			for i := 0; i < count; i++ {
				next := size(child(i))
				if next > maxFunctionSQLWork-total {
					return maxFunctionSQLWork + 1
				}
				total += next
			}
			return total
		default:
			return 16
		}
	}
	return size(reflect.ValueOf(expression))
}
