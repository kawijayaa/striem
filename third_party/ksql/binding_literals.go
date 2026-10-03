package ksql

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

func lowerLiteral(value *kql.LiteralExpression) sqlast.Expr {
	text := value.Text
	if value.Kind == kql.StringToken {
		return &sqlast.Literal{Kind: sqlast.StringLiteral, Value: stringLiteralValue(value)}
	}
	if strings.EqualFold(text, "true") || strings.EqualFold(text, "false") {
		return &sqlast.Literal{Kind: sqlast.BooleanLiteral, Value: strings.ToLower(text)}
	}
	if strings.EqualFold(text, "null") {
		return &sqlast.Literal{Kind: sqlast.NullLiteral}
	}
	if isTimespan(text) {
		return &sqlast.Literal{Kind: sqlast.IntervalLiteral, Value: text}
	}
	return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: text}
}

func unquoteKQL(value string) string {
	if decoded, err := kql.DecodeString(value); err == nil {
		return decoded
	}
	return value
}

func stringLiteralValue(value *kql.LiteralExpression) string {
	decoded, _ := kql.StringValue(value)
	return decoded
}

func isTimespan(value string) bool {
	if value == "" {
		return false
	}
	last := value[len(value)-1]
	return last >= 'a' && last <= 'z' || last >= 'A' && last <= 'Z'
}

func expressionText(expression kql.Expression) string {
	switch value := expression.(type) {
	case *kql.LiteralExpression:
		return stringLiteralValue(value)
	case *kql.NameExpression:
		return value.Name
	default:
		return ""
	}
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
		return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: realLiteralText(value, 64)}
	}
	valueOf := reflect.ValueOf(value)
	if valueOf.IsValid() {
		switch valueOf.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: strconv.FormatInt(valueOf.Int(), 10)}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: strconv.FormatUint(valueOf.Uint(), 10)}
		case reflect.Float32, reflect.Float64:
			return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: realLiteralText(valueOf.Float(), valueOf.Type().Bits())}
		}
	}
	return &sqlast.Literal{Kind: sqlast.StringLiteral, Value: fmt.Sprint(value)}
}

// Keep a floating-point SQL token even when its value is an integer. Losing
// the decimal point changes SQLite division, aggregation and result storage.
func realLiteralText(value float64, bits int) string {
	text := strconv.FormatFloat(value, 'g', -1, bits)
	if !strings.ContainsAny(text, ".eE") {
		text += ".0"
	}
	return text
}

func (s *compileState) literal(value *kql.LiteralExpression) boundExpr {
	text := value.Text
	if value.Kind == kql.StringToken {
		return boundExpr{SQL: s.bind(stringLiteralValue(value)), Type: TypeString}
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

func parseScalarType(value string) ScalarType {
	switch strings.ToLower(value) {
	case "bool", "boolean":
		return TypeBool
	case "int", "int32":
		return TypeInt
	case "long", "int64":
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
