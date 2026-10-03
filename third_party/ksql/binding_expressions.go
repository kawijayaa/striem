package ksql

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

func (s *compileState) expressions(expressions []kql.Expression) []sqlast.Expr {
	result := make([]sqlast.Expr, 0, len(expressions))
	for _, expression := range expressions {
		if value := s.expression(expression); value.SQL != nil {
			result = append(result, value.SQL)
		}
	}
	return result
}

func (s *compileState) expression(expression kql.Expression) (result boundExpr) {
	defer func() { result.rows = mergeRowDependencies(result.rows, s.expressionRows(result.SQL)) }()

	if expression == nil {
		return boundExpr{}
	}
	if s.functionDepth > 0 {
		s.functionWork++
		if s.functionWork > maxFunctionWork {
			if s.functionWork == maxFunctionWork+1 {
				s.bindError("KQLB0119", expression.NodeSpan(), "statement.let", "function expansion work limit exceeded")
			}
			return boundExpr{}
		}
	}
	switch value := expression.(type) {
	case *kql.NameExpression:
		if bound, ok := s.scalarLets[strings.ToLower(unquoteKQL(value.Name))]; ok && !s.hasInputName(unquoteKQL(value.Name)) {
			s.captureRows(bound.rows)
			bound.Name = unquoteKQL(value.Name)
			return bound
		}
		if !s.hasInputName(unquoteKQL(value.Name)) {
			if function := s.functions[strings.ToLower(unquoteKQL(value.Name))]; function != nil && len(function.parameters) == 0 {
				return s.callFunction(function, &kql.CallExpression{Function: value, Span: value.Span})
			}
		}
		return s.name(value)
	case *kql.LiteralExpression:
		return s.literal(value)
	case *kql.DynamicExpression:
		var text strings.Builder
		for i := 0; i < len(value.Tokens); i++ {
			token := value.Tokens[i]
			if token.Kind == kql.StringToken {
				var decoded strings.Builder
				decoded.WriteString(unquoteKQL(token.Text))
				for i+1 < len(value.Tokens) && value.Tokens[i+1].Kind == kql.StringToken {
					i++
					decoded.WriteString(unquoteKQL(value.Tokens[i].Text))
				}
				encoded, _ := json.Marshal(decoded.String())
				text.Write(encoded)
			} else {
				text.WriteString(token.Text)
			}
			// JSON signs must directly precede their number. Keep other token
			// boundaries so malformed adjacent scalars cannot become a valid value.
			sign := token.Text == "-" && i+1 < len(value.Tokens) && value.Tokens[i+1].Kind == kql.NumberToken
			if !sign {
				text.WriteByte(' ')
			}
		}
		data := strings.TrimSpace(text.String())
		if !json.Valid([]byte(data)) {
			s.bindError("KQLB0407", value.Span, "expression.dynamic", "dynamic literal must contain a valid JSON constant; typed constants are not yet supported")
			return boundExpr{}
		}
		var compact bytes.Buffer
		_ = json.Compact(&compact, []byte(data))
		expr := s.bind(compact.String())
		if data == "null" {
			expr = &sqlast.Literal{Kind: sqlast.NullLiteral}
		}
		return boundExpr{SQL: expr, Type: TypeDynamic}

	case *kql.StarExpression:
		return boundExpr{SQL: &sqlast.Star{}, Type: TypeUnknown, Name: "*"}
	case *kql.UnaryExpression:
		operand := s.expression(value.Operand)
		return boundExpr{SQL: &sqlast.Unary{Operator: value.Operator, Operand: operand.SQL}, Type: operand.Type}
	case *kql.BinaryExpression:
		return s.binary(value)
	case *kql.CallExpression:
		return s.call(value)
	case *kql.IndexExpression:
		if value.Path {
			if root, ok := value.Value.(*kql.NameExpression); ok && unquoteKQL(root.Name) == root.Name {
				if scope, found := s.qualifiers[strings.ToLower(root.Name)]; found {
					key, literal := value.Index.(*kql.LiteralExpression)
					if !literal || key.Kind != kql.StringToken {
						s.bindError("KQLB0404", value.Span, "expression.name", "qualified column selector requires a string name")
						return boundExpr{}
					}
					name, err := kql.StringValue(key)
					if err != nil {
						s.bindError("KQLB0404", value.Span, "expression.name", err.Error())
						return boundExpr{}
					}
					return s.qualifiedName(&kql.NameExpression{Span: value.Span}, scope, []string{name})
				}
			}
		}
		base, index := s.expression(value.Value), s.expression(value.Index)
		if base.Type != TypeDynamic && base.Type != TypeUnknown {
			s.bindError("KQLB0405", value.Span, "expression.index", "property access requires a dynamic value")
		}
		return boundExpr{SQL: &sqlast.Index{Value: base.SQL, Index: index.SQL, Numeric: index.Type == TypeInt || index.Type == TypeLong || index.Type == TypeBool}, Type: TypeDynamic, Name: indexedResultName(inferredName(value.Value, base.Name), literalIndexName(value.Index))}
	case *kql.NamedExpression:
		return s.expression(value.Value)
	case *kql.ListExpression:
		return boundExpr{SQL: &sqlast.List{Items: s.expressions(value.Items)}, Type: TypeDynamic}
	case *kql.RawExpression:
		s.error("KQLL0401", value.Span, "expression.raw", "expression syntax is preserved but cannot be lowered")
		return boundExpr{}
	default:
		s.error("KQLL0402", expression.NodeSpan(), "expression.unknown", fmt.Sprintf("unsupported expression node %T", expression))
		return boundExpr{}
	}
}
