package ksql

import (
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

func (s *compileState) call(value *kql.CallExpression) (result boundExpr) {
	name, ok := value.Function.(*kql.NameExpression)
	if !ok {
		s.error("KQLL0501", value.Span, "function.dynamic", "dynamic function references are not supported")
		return boundExpr{}
	}
	function := strings.ToLower(unquoteKQL(name.Name))
	if declaration, ok := s.functions[function]; ok {
		return s.callFunction(declaration, value)
	}
	if function == "materialize" {
		s.bindError("KQLB0524", value.Span, "function.materialize", "materialize returns a table and cannot be used as a scalar expression")
		return boundExpr{}
	}
	if function == "toscalar" {
		return s.toScalar(value)
	}
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
	firstName := ""
	if firstArgumentResultName(function) {
		defer func() {
			if result.SQL != nil {
				result.Name = firstName
			}
		}()
	}
	arguments := value.Args
	if function == "pack_array" && len(arguments) == 1 {
		if _, ok := arguments[0].(*kql.StarExpression); ok {
			if s.input.Unknown || len(s.input.Columns) == 0 {
				s.bindError("KQLB0512", value.Span, "function.pack_array", "pack_array(*) requires a known non-empty input schema")
				return boundExpr{}
			}
			for _, column := range s.input.Columns {
				args = append(args, s.columnIdentifier(column.Name))
				argTypes = append(argTypes, column.Type)
			}
			arguments = nil
		}
	}
	for _, argument := range arguments {
		bound := s.expression(argument)
		if bound.SQL != nil {
			if len(args) == 0 {
				firstName = inferredName(argument, bound.Name)
			}
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
