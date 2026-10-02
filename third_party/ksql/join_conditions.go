package ksql

import (
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

// Join conditions form a conjunction of common column names and explicit
// $left.column == $right.column keys. General scalar predicates are not keys.
func (s *compileState) bindJoinCondition(expression kql.Expression, left, right relationScope, keys *int) sqlast.Expr {
	if binary, ok := expression.(*kql.BinaryExpression); ok && strings.EqualFold(binary.Operator, "and") {
		s.use("operator.scalar.and", "equivalent", binary.Span)
		first := s.bindJoinCondition(binary.Left, left, right, keys)
		second := s.bindJoinCondition(binary.Right, left, right, keys)
		if first == nil || second == nil {
			return nil
		}
		return andExpression(first, second)
	}
	(*keys)++
	if maximum := s.compiler.limits.MaxJoinKeys; maximum > 0 && *keys > maximum {
		s.bindError("KQLB0312", expression.NodeSpan(), "operator.join", fmt.Sprintf("join exceeds MaxJoinKeys (%d)", maximum))
		return nil
	}
	if name, ok := expression.(*kql.NameExpression); ok && (unquoteKQL(name.Name) != name.Name || !strings.Contains(name.Name, ".")) {
		logical := unquoteKQL(name.Name)
		_, _, leftOK, leftAmbiguous := left.schema.Lookup(logical)
		_, _, rightOK, rightAmbiguous := right.schema.Lookup(logical)
		if !left.schema.Unknown && (!leftOK || leftAmbiguous) || !right.schema.Unknown && (!rightOK || rightAmbiguous) {
			s.bindError("KQLB0310", name.Span, "operator.join", "invalid join key "+logical)
			return nil
		}
		first := s.qualifiedName(name, left, []string{logical})
		second := s.qualifiedName(name, right, []string{logical})
		if !s.checkJoinKeyTypes(first, second, name.Span, true) {
			return nil
		}
		return &sqlast.Binary{Left: first.SQL, Operator: "=", Right: second.SQL}
	}
	binary, ok := expression.(*kql.BinaryExpression)
	if !ok || binary.Operator != "==" {
		s.bindError("KQLB0310", expression.NodeSpan(), "operator.join", "join requires column names or $left.column == $right.column equality keys")
		return nil
	}
	first, firstOK := s.explicitJoinColumn(binary.Left, "$left", left)
	second, secondOK := s.explicitJoinColumn(binary.Right, "$right", right)
	if !firstOK || !secondOK || !s.checkJoinKeyTypes(first, second, binary.Span, false) {
		return nil
	}
	s.use("operator.scalar.equal", "equivalent", binary.Span)
	return &sqlast.Binary{Left: first.SQL, Operator: "=", Right: second.SQL}
}

func (s *compileState) explicitJoinColumn(expression kql.Expression, side string, scope relationScope) (boundExpr, bool) {
	var column string
	valid := false
	switch value := expression.(type) {
	case *kql.NameExpression:
		if unquoteKQL(value.Name) == value.Name {
			parts := splitName(value.Name)
			if len(parts) == 2 && strings.EqualFold(parts[0], side) {
				column, valid = parts[1], true
			}
		}
	case *kql.IndexExpression:
		root, rootOK := value.Value.(*kql.NameExpression)
		key, keyOK := value.Index.(*kql.LiteralExpression)
		if value.Path && rootOK && strings.EqualFold(root.Name, side) && keyOK && key.Kind == kql.StringToken {
			var err error
			column, err = kql.StringValue(key)
			valid = err == nil
		}
	}
	if !valid {
		s.bindError("KQLB0310", expression.NodeSpan(), "operator.join", "join equality operand requires "+side+".column")
		return boundExpr{}, false
	}
	before := len(s.diagnostics)
	value := s.qualifiedName(&kql.NameExpression{Span: expression.NodeSpan()}, scope, []string{column})
	return value, value.SQL != nil && len(s.diagnostics) == before
}

func (s *compileState) checkJoinKeyTypes(left, right boundExpr, span kql.Span, common bool) bool {
	if left.SQL == nil || right.SQL == nil {
		return false
	}
	if !common && (left.Type == TypeDynamic || right.Type == TypeDynamic) {
		s.bindError("KQLB0315", span, "operator.join", "join keys cannot be dynamic; project scalar casts before joining")
		return false
	}
	first, second := left.Type, right.Type
	if !common {
		if first == TypeInt {
			first = TypeLong
		}
		if second == TypeInt {
			second = TypeLong
		}
	}
	if first != TypeUnknown && second != TypeUnknown && first != second {
		s.bindError("KQLB0311", span, "operator.join", "join key types do not match")
		return false
	}
	return true
}

func rightJoinKeys(condition sqlast.Expr, alias string) map[string]bool {
	keys := map[string]bool{}
	var visit func(sqlast.Expr)
	visit = func(expression sqlast.Expr) {
		if binary, ok := expression.(*sqlast.Binary); ok {
			if binary.Operator == "AND" {
				visit(binary.Left)
				visit(binary.Right)
			} else if binary.Operator == "=" {
				if identifier, ok := binary.Right.(*sqlast.Identifier); ok && len(identifier.Parts) == 2 && identifier.Parts[0] == alias {
					keys[identifier.Parts[1]] = true
				}
			}
		}
	}
	visit(condition)
	return keys
}
