// Package dialect renders dialect-neutral SQL syntax for concrete databases.
package dialect

import (
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql/sqlast"
)

// Dialect captures SQL syntax choices relevant to KQL lowering.
type Dialect interface {
	Name() string
	QuoteIdentifier(string) string
	Placeholder(int) string
	Boolean(bool) string
	Regex(left, right string, caseSensitive bool) (string, bool)
	JSONIndex(value, index string) string
	SafeCast(expression, target string) (string, bool)
	Series(column, from, to, step, alias string) (string, bool)
	ApplyLimit(selectSQL, limitSQL string) string
	SupportsNullOrdering() bool
	SupportsJoinUsing() bool
}

type standard struct{ name string }

// ANSI returns the portable SQL renderer. It avoids vendor-only features.
func ANSI() Dialect { return standard{name: "ansi"} }

func (d standard) Name() string                    { return d.name }
func (d standard) QuoteIdentifier(s string) string { return quoteDouble(s) }
func (d standard) Placeholder(index int) string    { return "?" }
func (d standard) Boolean(value bool) string {
	if value {
		return "TRUE"
	}
	return "FALSE"
}
func (d standard) Regex(_, _ string, _ bool) (string, bool) { return "", false }
func (d standard) JSONIndex(value, index string) string {
	return "JSON_QUERY(" + value + ", '$.' || " + index + ")"
}
func (d standard) SafeCast(_, _ string) (string, bool) { return "", false }
func (d standard) Series(_, _, _, _, _ string) (string, bool) {
	return "", false
}
func (d standard) ApplyLimit(sql, limit string) string {
	return sql + " FETCH FIRST " + limit + " ROWS ONLY"
}
func (d standard) SupportsNullOrdering() bool { return true }
func (d standard) SupportsJoinUsing() bool    { return true }

// Render converts a SQL AST to one formatted query string.
func Render(d Dialect, query sqlast.Query) (string, error) {
	r := renderer{dialect: d}
	return r.query(query)
}

type renderer struct{ dialect Dialect }

func (r renderer) query(query sqlast.Query) (string, error) {
	switch q := query.(type) {
	case *sqlast.Select:
		return r.selectQuery(q)
	case *sqlast.Union:
		if len(q.Queries) == 0 {
			return "", fmt.Errorf("union has no operands")
		}
		parts := make([]string, 0, len(q.Queries))
		for _, operand := range q.Queries {
			sql, err := r.query(operand)
			if err != nil {
				return "", err
			}
			if unionOperandNeedsWrapper(operand) {
				sql = "SELECT * FROM (" + sql + ") AS " + r.dialect.QuoteIdentifier("_ku")
			}
			parts = append(parts, sql)
		}
		operator := " UNION "
		if q.All {
			operator = " UNION ALL "
		}
		return strings.Join(parts, operator), nil
	default:
		return "", fmt.Errorf("unsupported SQL query node %T", query)
	}
}

func unionOperandNeedsWrapper(query sqlast.Query) bool {
	switch value := query.(type) {
	case *sqlast.Union:
		return true
	case *sqlast.Select:
		return value.Limit != nil || len(value.OrderBy) > 0
	default:
		return true
	}
}

func (r renderer) selectQuery(query *sqlast.Select) (string, error) {
	var b strings.Builder
	b.WriteString("SELECT ")
	if query.Distinct {
		b.WriteString("DISTINCT ")
	}
	if len(query.Projections) == 0 {
		b.WriteString("*")
	} else {
		for i, item := range query.Projections {
			if i > 0 {
				b.WriteString(", ")
			}
			expr, err := r.expr(item.Expr)
			if err != nil {
				return "", err
			}
			b.WriteString(expr)
			if item.Alias != "" {
				b.WriteString(" AS ")
				b.WriteString(r.dialect.QuoteIdentifier(item.Alias))
			}
		}
	}
	if query.From != nil {
		from, err := r.source(query.From)
		if err != nil {
			return "", err
		}
		b.WriteString(" FROM ")
		b.WriteString(from)
	}
	if query.Where != nil {
		where, err := r.expr(query.Where)
		if err != nil {
			return "", err
		}
		b.WriteString(" WHERE ")
		b.WriteString(where)
	}
	if len(query.GroupBy) > 0 {
		items, err := r.expressions(query.GroupBy)
		if err != nil {
			return "", err
		}
		b.WriteString(" GROUP BY ")
		b.WriteString(strings.Join(items, ", "))
	}
	if len(query.OrderBy) > 0 {
		b.WriteString(" ORDER BY ")
		for i, order := range query.OrderBy {
			if i > 0 {
				b.WriteString(", ")
			}
			expr, err := r.expr(order.Expr)
			if err != nil {
				return "", err
			}
			if order.Nulls != "" && !r.dialect.SupportsNullOrdering() {
				nullRank, nonNullRank := "0", "1"
				if order.Nulls == "last" {
					nullRank, nonNullRank = "1", "0"
				}
				b.WriteString("CASE WHEN ")
				b.WriteString(expr)
				b.WriteString(" IS NULL THEN ")
				b.WriteString(nullRank)
				b.WriteString(" ELSE ")
				b.WriteString(nonNullRank)
				b.WriteString(" END ASC, ")
			}
			b.WriteString(expr)
			if order.Direction != "" {
				b.WriteByte(' ')
				b.WriteString(strings.ToUpper(order.Direction))
			}
			if order.Nulls != "" && r.dialect.SupportsNullOrdering() {
				b.WriteString(" NULLS ")
				b.WriteString(strings.ToUpper(order.Nulls))
			}
		}
	}
	sql := b.String()
	if query.Limit != nil {
		limit, err := r.expr(query.Limit)
		if err != nil {
			return "", err
		}
		sql = r.dialect.ApplyLimit(sql, limit)
	}
	return sql, nil
}

func (r renderer) source(source sqlast.Source) (string, error) {
	switch s := source.(type) {
	case *sqlast.Table:
		parts := make([]string, len(s.Parts))
		for i, part := range s.Parts {
			parts[i] = r.dialect.QuoteIdentifier(part)
		}
		result := strings.Join(parts, ".")
		if s.Alias != "" {
			result += " AS " + r.dialect.QuoteIdentifier(s.Alias)
		}
		return result, nil
	case *sqlast.Subquery:
		query, err := r.query(s.Query)
		if err != nil {
			return "", err
		}
		return "(" + query + ") AS " + r.dialect.QuoteIdentifier(s.Alias), nil
	case *sqlast.Join:
		left, err := r.source(s.Left)
		if err != nil {
			return "", err
		}
		right, err := r.source(s.Right)
		if err != nil {
			return "", err
		}
		result := left + " " + strings.ToUpper(s.Kind) + " JOIN " + right
		if s.On != nil {
			on, err := r.expr(s.On)
			if err != nil {
				return "", err
			}
			result += " ON " + on
		} else if len(s.Using) > 0 && r.dialect.SupportsJoinUsing() {
			quoted := make([]string, len(s.Using))
			for i, column := range s.Using {
				quoted[i] = r.dialect.QuoteIdentifier(column)
			}
			result += " USING (" + strings.Join(quoted, ", ") + ")"
		} else if len(s.Using) > 0 {
			leftAlias, leftOK := sourceAlias(s.Left)
			rightAlias, rightOK := sourceAlias(s.Right)
			if !leftOK || !rightOK {
				return "", fmt.Errorf("dialect %s requires aliases to lower join keys", r.dialect.Name())
			}
			conditions := make([]string, len(s.Using))
			for i, column := range s.Using {
				quotedColumn := r.dialect.QuoteIdentifier(column)
				conditions[i] = r.dialect.QuoteIdentifier(leftAlias) + "." + quotedColumn + " = " + r.dialect.QuoteIdentifier(rightAlias) + "." + quotedColumn
			}
			result += " ON " + strings.Join(conditions, " AND ")
		}
		return result, nil
	case *sqlast.Values:
		if len(s.Columns) == 0 {
			return "", fmt.Errorf("values source requires columns")
		}
		if len(s.Rows) == 0 {
			items := make([]string, len(s.Columns))
			for i, column := range s.Columns {
				items[i] = "NULL AS " + r.dialect.QuoteIdentifier(column)
			}
			return "(SELECT " + strings.Join(items, ", ") + " WHERE 1 = 0) AS " + r.dialect.QuoteIdentifier(s.Alias), nil
		}
		rows := make([]string, len(s.Rows))
		for i, row := range s.Rows {
			values, err := r.expressions(row)
			if err != nil {
				return "", err
			}
			rows[i] = "(" + strings.Join(values, ", ") + ")"
		}
		columns := make([]string, len(s.Columns))
		for i, column := range s.Columns {
			columns[i] = r.dialect.QuoteIdentifier(column)
		}
		if r.dialect.Name() == "sqlite" {
			items := make([]string, len(s.Columns))
			for i, column := range s.Columns {
				items[i] = r.dialect.QuoteIdentifier(fmt.Sprintf("column%d", i+1)) + " AS " + r.dialect.QuoteIdentifier(column)
			}
			return "(SELECT " + strings.Join(items, ", ") + " FROM (VALUES " + strings.Join(rows, ", ") + ")) AS " + r.dialect.QuoteIdentifier(s.Alias), nil
		}
		return "(VALUES " + strings.Join(rows, ", ") + ") AS " + r.dialect.QuoteIdentifier(s.Alias) + "(" + strings.Join(columns, ", ") + ")", nil
	case *sqlast.Series:
		from, err := r.expr(s.From)
		if err != nil {
			return "", err
		}
		to, err := r.expr(s.To)
		if err != nil {
			return "", err
		}
		step, err := r.expr(s.Step)
		if err != nil {
			return "", err
		}
		column, alias := r.dialect.QuoteIdentifier(s.Column), r.dialect.QuoteIdentifier(s.Alias)
		if result, ok := r.dialect.Series(column, from, to, step, alias); ok {
			return result, nil
		}
		return "", fmt.Errorf("dialect %s has no range-series source", r.dialect.Name())
	case *sqlast.JSONEach:
		value, err := r.expr(s.Value)
		if err != nil {
			return "", err
		}
		if capability, ok := r.dialect.(interface {
			JSONEach(string, string) (string, bool)
		}); ok {
			result, supported := capability.JSONEach(value, r.dialect.QuoteIdentifier(s.Alias))
			if supported {
				return result, nil
			}
		}
		return "", fmt.Errorf("dialect %s has no JSON array expansion", r.dialect.Name())
	default:
		return "", fmt.Errorf("unsupported SQL source node %T", source)
	}
}

func sourceAlias(source sqlast.Source) (string, bool) {
	switch value := source.(type) {
	case *sqlast.Table:
		return value.Alias, value.Alias != ""
	case *sqlast.Subquery:
		return value.Alias, value.Alias != ""
	case *sqlast.Values:
		return value.Alias, value.Alias != ""
	case *sqlast.Series:
		return value.Alias, value.Alias != ""
	default:
		return "", false
	}
}

func (r renderer) expr(expression sqlast.Expr) (string, error) {
	switch e := expression.(type) {
	case *sqlast.Identifier:
		parts := make([]string, len(e.Parts))
		for i, part := range e.Parts {
			parts[i] = r.dialect.QuoteIdentifier(part)
		}
		return strings.Join(parts, "."), nil
	case *sqlast.Literal:
		return r.literal(e)
	case *sqlast.Parameter:
		return r.dialect.Placeholder(e.Index), nil
	case *sqlast.Star:
		return "*", nil
	case *sqlast.QualifiedStar:
		parts := make([]string, len(e.Parts))
		for i, part := range e.Parts {
			parts[i] = r.dialect.QuoteIdentifier(part)
		}
		return strings.Join(parts, ".") + ".*", nil
	case *sqlast.Random:
		if capability, ok := r.dialect.(interface{ Random() (string, bool) }); ok {
			if value, supported := capability.Random(); supported {
				return value, nil
			}
		}
		return "", fmt.Errorf("dialect %s has no random ordering expression", r.dialect.Name())
	case *sqlast.Unary:
		operand, err := r.expr(e.Operand)
		return e.Operator + operand, err
	case *sqlast.Binary:
		left, err := r.expr(e.Left)
		if err != nil {
			return "", err
		}
		right, err := r.expr(e.Right)
		if err != nil {
			return "", err
		}
		return "(" + left + " " + e.Operator + " " + right + ")", nil
	case *sqlast.Call:
		args, err := r.expressions(e.Args)
		if err != nil {
			return "", err
		}
		if strings.EqualFold(e.Name, "concat") {
			if capability, ok := r.dialect.(interface {
				Concat([]string) (string, bool)
			}); ok {
				if result, supported := capability.Concat(args); supported {
					return result, nil
				}
			}
		}
		return strings.ToUpper(e.Name) + "(" + strings.Join(args, ", ") + ")", nil
	case *sqlast.Window:
		call, err := r.expr(e.Expr)
		if err != nil {
			return "", err
		}
		clauses := []string{}
		if len(e.PartitionBy) > 0 {
			parts, err := r.expressions(e.PartitionBy)
			if err != nil {
				return "", err
			}
			clauses = append(clauses, "PARTITION BY "+strings.Join(parts, ", "))
		}
		if len(e.OrderBy) > 0 {
			parts := make([]string, 0, len(e.OrderBy))
			for _, order := range e.OrderBy {
				expression, err := r.expr(order.Expr)
				if err != nil {
					return "", err
				}
				direction := strings.ToUpper(order.Direction)
				if direction != "" && direction != "ASC" && direction != "DESC" {
					return "", fmt.Errorf("invalid window order direction %q", direction)
				}
				if order.Nulls != "" {
					return "", fmt.Errorf("explicit window null ordering is not yet supported")
				}
				if direction != "" {
					expression += " " + direction
				}
				parts = append(parts, expression)
			}
			clauses = append(clauses, "ORDER BY "+strings.Join(parts, ", "))
		}
		return call + " OVER (" + strings.Join(clauses, " ") + ")", nil
	case *sqlast.List:
		items, err := r.expressions(e.Items)
		return "(" + strings.Join(items, ", ") + ")", err
	case *sqlast.Case:
		var b strings.Builder
		b.WriteString("CASE")
		for _, branch := range e.Branches {
			condition, err := r.expr(branch.Condition)
			if err != nil {
				return "", err
			}
			result, err := r.expr(branch.Result)
			if err != nil {
				return "", err
			}
			b.WriteString(" WHEN ")
			b.WriteString(condition)
			b.WriteString(" THEN ")
			b.WriteString(result)
		}
		if e.Else != nil {
			otherwise, err := r.expr(e.Else)
			if err != nil {
				return "", err
			}
			b.WriteString(" ELSE ")
			b.WriteString(otherwise)
		}
		b.WriteString(" END")
		return b.String(), nil
	case *sqlast.Cast:
		expression, err := r.expr(e.Expr)
		if err != nil {
			return "", err
		}
		if e.Safe {
			if result, ok := r.dialect.SafeCast(expression, e.Type); ok {
				return result, nil
			}
			return "", fmt.Errorf("dialect %s has no safe cast", r.dialect.Name())
		}
		return "CAST(" + expression + " AS " + e.Type + ")", nil
	case *sqlast.Index:
		if capability, ok := r.dialect.(interface {
			JSONIndexPath(value string, keys []string) (string, bool)
		}); ok {
			if base, keys, ok := literalJSONIndexPath(e); ok {
				value, err := r.expr(base)
				if err != nil {
					return "", err
				}
				if result, supported := capability.JSONIndexPath(value, keys); supported {
					return result, nil
				}
			}
		}
		value, err := r.expr(e.Value)
		if err != nil {
			return "", err
		}
		index, err := r.expr(e.Index)
		if err != nil {
			return "", err
		}
		if capability, ok := r.dialect.(interface {
			JSONIndexExpression(value, index string, numeric bool) string
		}); ok {
			numeric := false
			switch indexNode := e.Index.(type) {
			case *sqlast.Literal:
				numeric = indexNode.Kind == sqlast.NumberLiteral
			case *sqlast.Parameter:
				numeric = indexNode.Kind == sqlast.NumberLiteral
			}
			return capability.JSONIndexExpression(value, index, numeric), nil
		}
		return r.dialect.JSONIndex(value, index), nil
	case *sqlast.Regex:
		value, err := r.expr(e.Value)
		if err != nil {
			return "", err
		}
		pattern, err := r.expr(e.Pattern)
		if err != nil {
			return "", err
		}
		if result, ok := r.dialect.Regex(value, pattern, e.CaseSensitive); ok {
			return result, nil
		}
		return "", fmt.Errorf("dialect %s has no regular expression operator", r.dialect.Name())
	case *sqlast.Exists:
		query, err := r.query(e.Query)
		if err != nil {
			return "", err
		}
		return "EXISTS (" + query + ")", nil
	case *sqlast.Filtered:
		expression, err := r.expr(e.Expr)
		if err != nil {
			return "", err
		}
		where, err := r.expr(e.Where)
		if err != nil {
			return "", err
		}
		return expression + " FILTER (WHERE " + where + ")", nil
	default:
		return "", fmt.Errorf("unsupported SQL expression node %T", expression)
	}
}

func literalJSONIndexPath(expression *sqlast.Index) (sqlast.Expr, []string, bool) {
	var reversed []string
	var current sqlast.Expr = expression
	for {
		index, ok := current.(*sqlast.Index)
		if !ok {
			break
		}
		literal, ok := index.Index.(*sqlast.Literal)
		if !ok || literal.Kind != sqlast.StringLiteral {
			break
		}
		reversed = append(reversed, literal.Value)
		current = index.Value
	}
	if len(reversed) < 2 {
		return nil, nil, false
	}
	keys := make([]string, len(reversed))
	for i := range reversed {
		keys[len(reversed)-1-i] = reversed[i]
	}
	return current, keys, true
}

func (r renderer) expressions(expressions []sqlast.Expr) ([]string, error) {
	result := make([]string, len(expressions))
	for i, expression := range expressions {
		value, err := r.expr(expression)
		if err != nil {
			return nil, err
		}
		result[i] = value
	}
	return result, nil
}

func (r renderer) literal(literal *sqlast.Literal) (string, error) {
	switch literal.Kind {
	case sqlast.NullLiteral:
		return "NULL", nil
	case sqlast.NumberLiteral:
		return literal.Value, nil
	case sqlast.StringLiteral:
		return quoteString(literal.Value), nil
	case sqlast.DateTimeLiteral:
		return "TIMESTAMP " + quoteString(literal.Value), nil
	case sqlast.IntervalLiteral:
		return "INTERVAL " + quoteString(literal.Value), nil
	case sqlast.JSONLiteral:
		return quoteString(literal.Value), nil
	case sqlast.BooleanLiteral:
		return r.dialect.Boolean(strings.EqualFold(literal.Value, "true")), nil
	default:
		return "", fmt.Errorf("invalid SQL literal kind %d", literal.Kind)
	}
}

func quoteDouble(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
func quoteString(value string) string { return `'` + strings.ReplaceAll(value, `'`, `''`) + `'` }
