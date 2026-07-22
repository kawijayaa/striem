package kql

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/kawijayaa/striem/internal/eventtime"
)

type CompiledQuery struct {
	SQL     string
	Args    []any
	Columns []string
}

type TableCatalog map[string]int64

type compiler struct {
	args      []any
	columns   map[string]struct{}
	variables map[string]string
	constants map[string]any
	tables    TableCatalog
	now       time.Time
}

type relation struct {
	SQL     string
	Columns []string
}

var eventColumns = []string{"TimeGenerated", "Source", "EventType", "Host", "User", "Message", "RawData"}

func Compile(query Query, now time.Time, catalogs ...TableCatalog) (CompiledQuery, error) {
	var tables TableCatalog
	if len(catalogs) > 0 {
		tables = catalogs[0]
	}
	c := &compiler{
		columns:   make(map[string]struct{}),
		variables: make(map[string]string),
		constants: make(map[string]any),
		tables:    tables,
		now:       now.UTC(),
	}
	eventColumnSet := columnSet(eventColumns)
	for _, binding := range query.Bindings {
		if _, exists := c.variables[binding.Name]; exists {
			return CompiledQuery{}, errorAt(binding.At, "variable %q is declared more than once", binding.Name)
		}
		if _, exists := eventColumnSet[binding.Name]; exists {
			return CompiledQuery{}, errorAt(binding.At, "variable %q conflicts with a table column", binding.Name)
		}
		value, err := c.compileExpression(binding.Expression, false)
		if err != nil {
			return CompiledQuery{}, err
		}
		c.variables[binding.Name] = value
		if constant, ok := c.evaluateConstant(binding.Expression); ok {
			c.constants[binding.Name] = constant
		}
	}
	compiled, err := c.compilePipeline(query)
	if err != nil {
		return CompiledQuery{}, err
	}
	limit := c.bind(int64(1000))
	compiled.SQL = fmt.Sprintf("SELECT * FROM (%s) AS result LIMIT %s", compiled.SQL, limit)
	return CompiledQuery{SQL: compiled.SQL, Args: c.args, Columns: compiled.Columns}, nil
}

func (c *compiler) compilePipeline(query Query) (relation, error) {
	datasetID, tableFound := c.tables[query.Source]
	if query.Source != "Events" && !tableFound {
		return relation{}, errorAt(query.SourceAt, "unknown table %q", query.Source)
	}
	c.columns = columnSet(eventColumns)
	sqlText := `SELECT time_generated AS "TimeGenerated", source AS "Source", event_type AS "EventType", host AS "Host", username AS "User", message AS "Message", raw_data AS "RawData" FROM events`
	if query.Source != "Events" {
		sqlText += " WHERE dataset_id = " + c.bind(datasetID)
	}
	columns := append([]string(nil), eventColumns...)

	for _, rawOperator := range query.Operators {
		switch operator := rawOperator.(type) {
		case WhereOperator:
			expr, err := c.compileExpression(operator.Expression, false)
			if err != nil {
				return relation{}, err
			}
			sqlText = fmt.Sprintf("SELECT * FROM (%s) AS q WHERE %s", sqlText, expr)
		case ProjectOperator:
			selects, names, err := c.compileNamed(operator.Items, false)
			if err != nil {
				return relation{}, err
			}
			sqlText = fmt.Sprintf("SELECT %s FROM (%s) AS q", strings.Join(selects, ", "), sqlText)
			columns, c.columns = names, columnSet(names)
		case ExtendOperator:
			selects, names, err := c.compileNamed(operator.Items, false)
			if err != nil {
				return relation{}, err
			}
			replaced := columnSet(names)
			preserved := make([]string, 0, len(columns))
			baseSelects := make([]string, 0, len(columns))
			for _, name := range columns {
				if _, replace := replaced[name]; replace {
					continue
				}
				preserved = append(preserved, name)
				baseSelects = append(baseSelects, "q."+quoteIdentifier(name)+" AS "+quoteIdentifier(name))
			}
			selects = append(baseSelects, selects...)
			sqlText = fmt.Sprintf("SELECT %s FROM (%s) AS q", strings.Join(selects, ", "), sqlText)
			columns = append(preserved, names...)
			for _, name := range names {
				c.columns[name] = struct{}{}
			}
		case SummarizeOperator:
			for _, item := range operator.Aggregates {
				if err := c.validateSummarizeExpression(item.Expression); err != nil {
					return relation{}, err
				}
			}
			aggregates, aggregateNames, err := c.compileNamed(operator.Aggregates, true)
			if err != nil {
				return relation{}, err
			}
			groups, groupNames, err := c.compileNamed(operator.Groups, false)
			if err != nil {
				return relation{}, err
			}
			selects := append(groups, aggregates...)
			sqlText = fmt.Sprintf("SELECT %s FROM (%s) AS q", strings.Join(selects, ", "), sqlText)
			if len(groups) > 0 {
				positions := make([]string, len(groups))
				for index := range groups {
					positions[index] = fmt.Sprintf("%d", index+1)
				}
				sqlText += " GROUP BY " + strings.Join(positions, ", ")
			}
			columns = append(groupNames, aggregateNames...)
			c.columns = columnSet(columns)
		case DistinctOperator:
			selects, names, err := c.compileNamed(operator.Items, false)
			if err != nil {
				return relation{}, err
			}
			sqlText = fmt.Sprintf("SELECT DISTINCT %s FROM (%s) AS q", strings.Join(selects, ", "), sqlText)
			columns, c.columns = names, columnSet(names)
		case SortOperator:
			terms := make([]string, 0, len(operator.Terms))
			for _, term := range operator.Terms {
				expr, err := c.compileExpression(term.Expression, false)
				if err != nil {
					return relation{}, err
				}
				direction := "ASC"
				if term.Descending {
					direction = "DESC"
				}
				terms = append(terms, expr+" "+direction)
			}
			sqlText = fmt.Sprintf("SELECT * FROM (%s) AS q ORDER BY %s", sqlText, strings.Join(terms, ", "))
		case TakeOperator:
			count, err := c.evaluateRowLimit(operator.Count)
			if err != nil {
				return relation{}, err
			}
			limit := c.bind(count)
			sqlText = fmt.Sprintf("SELECT * FROM (%s) AS q LIMIT %s", sqlText, limit)
		case TopOperator:
			expr, err := c.compileExpression(operator.Term.Expression, false)
			if err != nil {
				return relation{}, err
			}
			direction := "DESC"
			if !operator.Term.Descending {
				direction = "ASC"
			}
			count, err := c.evaluateRowLimit(operator.Count)
			if err != nil {
				return relation{}, err
			}
			limit := c.bind(count)
			sqlText = fmt.Sprintf("SELECT * FROM (%s) AS q ORDER BY %s %s LIMIT %s", sqlText, expr, direction, limit)
		case CountOperator:
			sqlText = fmt.Sprintf(`SELECT COUNT(*) AS "Count" FROM (%s) AS q`, sqlText)
			columns, c.columns = []string{"Count"}, columnSet([]string{"Count"})
		case UnionOperator:
			combined, err := c.compileUnion(relation{SQL: sqlText, Columns: columns}, operator)
			if err != nil {
				return relation{}, err
			}
			sqlText, columns = combined.SQL, combined.Columns
			c.columns = columnSet(columns)
		case JoinOperator:
			joined, err := c.compileJoin(relation{SQL: sqlText, Columns: columns}, operator)
			if err != nil {
				return relation{}, err
			}
			sqlText, columns = joined.SQL, joined.Columns
			c.columns = columnSet(columns)
		}
	}
	return relation{SQL: sqlText, Columns: columns}, nil
}

func (c *compiler) compileUnion(left relation, operator UnionOperator) (relation, error) {
	arms := []string{projectRelation(left, left.Columns, "u0")}
	leftSet := columnSet(left.Columns)
	for index, query := range operator.Queries {
		right, err := c.compilePipeline(query)
		if err != nil {
			return relation{}, err
		}
		rightSet := columnSet(right.Columns)
		if len(right.Columns) != len(left.Columns) {
			return relation{}, errorAt(query.SourceAt, "union query must have the same columns as its left side")
		}
		for _, name := range left.Columns {
			if _, exists := rightSet[name]; !exists {
				return relation{}, errorAt(query.SourceAt, "union query is missing column %q", name)
			}
		}
		for _, name := range right.Columns {
			if _, exists := leftSet[name]; !exists {
				return relation{}, errorAt(query.SourceAt, "union query has unexpected column %q", name)
			}
		}
		arms = append(arms, projectRelation(right, left.Columns, fmt.Sprintf("u%d", index+1)))
	}
	return relation{SQL: strings.Join(arms, " UNION ALL "), Columns: append([]string(nil), left.Columns...)}, nil
}

func (c *compiler) compileJoin(left relation, operator JoinOperator) (relation, error) {
	right, err := c.compilePipeline(operator.Right)
	if err != nil {
		return relation{}, err
	}
	leftSet, rightSet := columnSet(left.Columns), columnSet(right.Columns)
	keys := make(map[string]struct{}, len(operator.Keys))
	conditions := make([]string, 0, len(operator.Keys))
	for _, key := range operator.Keys {
		if _, duplicate := keys[key.Name]; duplicate {
			return relation{}, errorAt(key.At, "join column %q is specified more than once", key.Name)
		}
		keys[key.Name] = struct{}{}
		if _, exists := leftSet[key.Name]; !exists {
			return relation{}, errorAt(key.At, "join column %q does not exist on the left side", key.Name)
		}
		if _, exists := rightSet[key.Name]; !exists {
			return relation{}, errorAt(key.At, "join column %q does not exist on the right side", key.Name)
		}
		conditions = append(conditions, "l."+quoteIdentifier(key.Name)+" = r."+quoteIdentifier(key.Name))
	}
	selects := make([]string, 0, len(left.Columns)+len(right.Columns))
	columns := append([]string(nil), left.Columns...)
	used := columnSet(columns)
	for _, name := range left.Columns {
		selects = append(selects, "l."+quoteIdentifier(name)+" AS "+quoteIdentifier(name))
	}
	for _, name := range right.Columns {
		if _, isKey := keys[name]; isKey {
			continue
		}
		output := availableColumnName(name, used)
		used[output] = struct{}{}
		columns = append(columns, output)
		selects = append(selects, "r."+quoteIdentifier(name)+" AS "+quoteIdentifier(output))
	}
	joinSQL := "INNER JOIN"
	if operator.Kind == JoinLeftOuter {
		joinSQL = "LEFT OUTER JOIN"
	}
	sqlText := fmt.Sprintf("SELECT %s FROM (%s) AS l %s (%s) AS r ON %s", strings.Join(selects, ", "), left.SQL, joinSQL, right.SQL, strings.Join(conditions, " AND "))
	return relation{SQL: sqlText, Columns: columns}, nil
}

func projectRelation(value relation, columns []string, alias string) string {
	selects := make([]string, len(columns))
	for index, name := range columns {
		selects[index] = alias + "." + quoteIdentifier(name) + " AS " + quoteIdentifier(name)
	}
	return fmt.Sprintf("SELECT %s FROM (%s) AS %s", strings.Join(selects, ", "), value.SQL, alias)
}

func availableColumnName(name string, used map[string]struct{}) string {
	if _, exists := used[name]; !exists {
		return name
	}
	for suffix := 1; suffix <= 1000; suffix++ {
		candidate := fmt.Sprintf("%s%d", name, suffix)
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
	panic("too many column name collisions")
}

func (c *compiler) compileNamed(items []NamedExpression, aggregate bool) ([]string, []string, error) {
	selects := make([]string, 0, len(items))
	names := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, exists := seen[item.Name]; exists {
			return nil, nil, errorAt(item.At, "column %q is specified more than once", item.Name)
		}
		seen[item.Name] = struct{}{}
		expr, err := c.compileExpression(item.Expression, aggregate)
		if err != nil {
			return nil, nil, err
		}
		selects = append(selects, fmt.Sprintf(`%s AS %s`, expr, quoteIdentifier(item.Name)))
		names = append(names, item.Name)
	}
	return selects, names, nil
}

func (c *compiler) compileExpression(raw Expression, aggregate bool) (string, error) {
	switch expr := raw.(type) {
	case IdentifierExpression:
		if len(expr.Parts) == 1 {
			if _, exists := c.columns[expr.Parts[0]]; exists {
				return "q." + quoteIdentifier(expr.Parts[0]), nil
			}
			if variable, exists := c.variables[expr.Parts[0]]; exists {
				return "(" + variable + ")", nil
			}
			return "", errorAt(expr.At, "unknown column or variable %q", expr.Parts[0])
		}
		root := expr.Parts[0]
		base := ""
		if _, exists := c.columns[root]; exists {
			base = "q." + quoteIdentifier(root)
		} else if variable, exists := c.variables[root]; exists {
			base = "(" + variable + ")"
		} else {
			return "", errorAt(expr.At, "unknown column or variable %q", root)
		}
		path := "$"
		for _, part := range expr.Parts[1:] {
			path += `."` + strings.ReplaceAll(part, `"`, `\"`) + `"`
		}
		return `json_extract(` + base + `, ` + c.bind(path) + `)`, nil
	case LiteralExpression:
		return c.bind(expr.Value), nil
	case DurationExpression:
		return "", errorAt(expr.At, "duration literals are only valid in ago() and bin()")
	case UnaryExpression:
		operand, err := c.compileExpression(expr.Operand, aggregate)
		if err != nil {
			return "", err
		}
		switch expr.Operator {
		case "not":
			return "(NOT " + operand + ")", nil
		case "+", "-":
			return "(" + expr.Operator + operand + ")", nil
		}
	case BinaryExpression:
		if (expr.Operator == "==" || expr.Operator == "!=") && (isNullLiteral(expr.Left) || isNullLiteral(expr.Right)) {
			operand := expr.Left
			if isNullLiteral(operand) {
				operand = expr.Right
			}
			value, err := c.compileExpression(operand, aggregate)
			if err != nil {
				return "", err
			}
			if expr.Operator == "==" {
				return "(" + value + " IS NULL)", nil
			}
			return "(" + value + " IS NOT NULL)", nil
		}
		left, err := c.compileExpression(expr.Left, aggregate)
		if err != nil {
			return "", err
		}
		if expr.Operator == "in" {
			list := expr.Right.(ListExpression)
			values := make([]string, 0, len(list.Items))
			for _, item := range list.Items {
				value, itemErr := c.compileExpression(item, aggregate)
				if itemErr != nil {
					return "", itemErr
				}
				values = append(values, value)
			}
			return fmt.Sprintf("(%s IN (%s))", left, strings.Join(values, ", ")), nil
		}
		right, err := c.compileExpression(expr.Right, aggregate)
		if err != nil {
			return "", err
		}
		switch expr.Operator {
		case "and":
			return "(" + left + " AND " + right + ")", nil
		case "or":
			return "(" + left + " OR " + right + ")", nil
		case "==", "!=", "<", "<=", ">", ">=":
			op := expr.Operator
			if op == "==" {
				op = "="
			}
			return "(" + left + " " + op + " " + right + ")", nil
		case "+", "-", "*", "/", "%":
			return "(" + left + " " + expr.Operator + " " + right + ")", nil
		case "contains":
			return "(instr(lower(CAST(" + left + " AS TEXT)), lower(CAST(" + right + " AS TEXT))) > 0)", nil
		case "startswith":
			return "(substr(lower(CAST(" + left + " AS TEXT)), 1, length(CAST(" + right + " AS TEXT))) = lower(CAST(" + right + " AS TEXT)))", nil
		case "endswith":
			return "(substr(lower(CAST(" + left + " AS TEXT)), -length(CAST(" + right + " AS TEXT))) = lower(CAST(" + right + " AS TEXT)))", nil
		}
	case FunctionExpression:
		return c.compileFunction(expr, aggregate)
	}
	return "", errorAt(raw.position(), "unsupported expression")
}

func (c *compiler) compileFunction(expr FunctionExpression, aggregate bool) (string, error) {
	compileArgs := func(expected int) ([]string, error) {
		if len(expr.Arguments) != expected {
			return nil, errorAt(expr.At, "%s() expects %d argument(s)", expr.Name, expected)
		}
		values := make([]string, expected)
		for index, argument := range expr.Arguments {
			value, err := c.compileExpression(argument, false)
			if err != nil {
				return nil, err
			}
			values[index] = value
		}
		return values, nil
	}
	switch expr.Name {
	case "now":
		if len(expr.Arguments) != 0 {
			return "", errorAt(expr.At, "now() expects no arguments")
		}
		return c.bind(eventtime.Format(c.now)), nil
	case "ago":
		if len(expr.Arguments) != 1 {
			return "", errorAt(expr.At, "ago() expects one duration")
		}
		duration, ok := expr.Arguments[0].(DurationExpression)
		if !ok {
			return "", errorAt(expr.At, "ago() requires a duration such as 15m")
		}
		return c.bind(eventtime.Format(c.now.Add(-duration.Value))), nil
	case "datetime":
		if len(expr.Arguments) != 1 {
			return "", errorAt(expr.At, "datetime() expects one string")
		}
		literal, ok := expr.Arguments[0].(LiteralExpression)
		if !ok {
			return "", errorAt(expr.At, "datetime() requires a string")
		}
		text, ok := literal.Value.(string)
		if !ok {
			return "", errorAt(expr.At, "datetime() requires a string")
		}
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return "", errorAt(expr.At, "invalid RFC3339 datetime")
		}
		return c.bind(eventtime.Format(parsed)), nil
	case "bin":
		if len(expr.Arguments) != 2 {
			return "", errorAt(expr.At, "bin() expects a value and duration")
		}
		value, err := c.compileExpression(expr.Arguments[0], false)
		if err != nil {
			return "", err
		}
		duration, ok := expr.Arguments[1].(DurationExpression)
		if !ok || duration.Value < time.Second {
			return "", errorAt(expr.At, "bin() requires a duration of at least 1s")
		}
		seconds := int64(duration.Value / time.Second)
		first, second := c.bind(seconds), c.bind(seconds)
		return "strftime('%Y-%m-%dT%H:%M:%SZ', (CAST(strftime('%s', " + value + ") AS INTEGER) / " + first + ") * " + second + ", 'unixepoch')", nil
	case "tostring", "toint", "tolower", "toupper", "isnull", "isnotnull", "parse_json", "strlen":
		args, err := compileArgs(1)
		if err != nil {
			return "", err
		}
		switch expr.Name {
		case "tostring":
			return "CAST(" + args[0] + " AS TEXT)", nil
		case "toint":
			return "CAST(" + args[0] + " AS INTEGER)", nil
		case "tolower":
			return "lower(CAST(" + args[0] + " AS TEXT))", nil
		case "toupper":
			return "upper(CAST(" + args[0] + " AS TEXT))", nil
		case "isnull":
			return "(" + args[0] + " IS NULL)", nil
		case "isnotnull":
			return "(" + args[0] + " IS NOT NULL)", nil
		case "parse_json":
			return "json(" + args[0] + ")", nil
		case "strlen":
			return "length(CAST(" + args[0] + " AS TEXT))", nil
		}
	case "iff":
		args, err := compileArgs(3)
		if err != nil {
			return "", err
		}
		return "(CASE WHEN " + args[0] + " THEN " + args[1] + " ELSE " + args[2] + " END)", nil
	case "coalesce":
		if len(expr.Arguments) < 2 {
			return "", errorAt(expr.At, "coalesce() expects at least 2 arguments")
		}
		args := make([]string, len(expr.Arguments))
		for index, argument := range expr.Arguments {
			value, err := c.compileExpression(argument, false)
			if err != nil {
				return "", err
			}
			args[index] = value
		}
		return "COALESCE(" + strings.Join(args, ", ") + ")", nil
	case "substring":
		if len(expr.Arguments) != 2 && len(expr.Arguments) != 3 {
			return "", errorAt(expr.At, "substring() expects 2 or 3 arguments")
		}
		args := make([]string, len(expr.Arguments))
		for index, argument := range expr.Arguments {
			value, err := c.compileExpression(argument, false)
			if err != nil {
				return "", err
			}
			args[index] = value
		}
		result := "substr(CAST(" + args[0] + " AS TEXT), (" + args[1] + ") + 1"
		if len(args) == 3 {
			result += ", " + args[2]
		}
		return result + ")", nil
	case "strcat":
		if len(expr.Arguments) < 2 {
			return "", errorAt(expr.At, "strcat() expects at least 2 arguments")
		}
		args := make([]string, len(expr.Arguments))
		for index, argument := range expr.Arguments {
			value, err := c.compileExpression(argument, false)
			if err != nil {
				return "", err
			}
			args[index] = "COALESCE(CAST(" + value + " AS TEXT), '')"
		}
		return "(" + strings.Join(args, " || ") + ")", nil
	case "count", "dcount", "sum", "min", "max", "avg", "countif":
		if !aggregate {
			return "", errorAt(expr.At, "%s() is only supported in summarize", expr.Name)
		}
		if expr.Name == "count" {
			if len(expr.Arguments) != 0 {
				return "", errorAt(expr.At, "count() expects no arguments")
			}
			return "COUNT(*)", nil
		}
		args, err := compileArgs(1)
		if err != nil {
			return "", err
		}
		switch expr.Name {
		case "dcount":
			return "COUNT(DISTINCT " + args[0] + ")", nil
		case "countif":
			return "SUM(CASE WHEN " + args[0] + " THEN 1 ELSE 0 END)", nil
		default:
			return strings.ToUpper(expr.Name) + "(" + args[0] + ")", nil
		}
	}
	return "", errorAt(expr.At, "function %q is not supported", expr.Name)
}

func isNullLiteral(expression Expression) bool {
	literal, ok := expression.(LiteralExpression)
	return ok && literal.Value == nil
}

func (c *compiler) evaluateRowLimit(expression Expression) (int64, error) {
	value, ok := c.evaluateConstant(expression)
	if !ok {
		return 0, errorAt(expression.position(), "row limit must be a constant integer")
	}
	number, ok := numericConstant(value)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number {
		return 0, errorAt(expression.position(), "row limit must be a constant integer")
	}
	if number < 1 || number > 1000 {
		return 0, errorAt(expression.position(), "row limit must be between 1 and 1000")
	}
	return int64(number), nil
}

func (c *compiler) evaluateConstant(expression Expression) (any, bool) {
	switch expr := expression.(type) {
	case LiteralExpression:
		return expr.Value, true
	case IdentifierExpression:
		if len(expr.Parts) != 1 {
			return nil, false
		}
		value, exists := c.constants[expr.Parts[0]]
		return value, exists
	case UnaryExpression:
		value, ok := c.evaluateConstant(expr.Operand)
		number, numeric := numericConstant(value)
		if !ok || !numeric {
			return nil, false
		}
		switch expr.Operator {
		case "+":
			return number, true
		case "-":
			return -number, true
		default:
			return nil, false
		}
	case BinaryExpression:
		leftValue, leftOK := c.evaluateConstant(expr.Left)
		rightValue, rightOK := c.evaluateConstant(expr.Right)
		left, leftNumeric := numericConstant(leftValue)
		right, rightNumeric := numericConstant(rightValue)
		if !leftOK || !rightOK || !leftNumeric || !rightNumeric {
			return nil, false
		}
		switch expr.Operator {
		case "+":
			return left + right, true
		case "-":
			return left - right, true
		case "*":
			return left * right, true
		case "/":
			if right == 0 {
				return nil, false
			}
			return left / right, true
		case "%":
			if right == 0 {
				return nil, false
			}
			return math.Mod(left, right), true
		default:
			return nil, false
		}
	default:
		return nil, false
	}
}

func numericConstant(value any) (float64, bool) {
	number, ok := value.(float64)
	return number, ok
}

func (c *compiler) validateSummarizeExpression(expression Expression) error {
	foundAggregate, err := c.inspectSummarizeExpression(expression, false)
	if err != nil {
		return err
	}
	if !foundAggregate {
		return errorAt(expression.position(), "summarize expressions must contain an aggregation function")
	}
	return nil
}

func (c *compiler) inspectSummarizeExpression(expression Expression, insideAggregate bool) (bool, error) {
	switch expr := expression.(type) {
	case IdentifierExpression:
		if len(expr.Parts) == 1 {
			if _, exists := c.variables[expr.Parts[0]]; exists {
				return false, nil
			}
		}
		if !insideAggregate {
			return false, errorAt(expr.At, "column %q must be grouped or aggregated", expr.Parts[0])
		}
		return false, nil
	case UnaryExpression:
		return c.inspectSummarizeExpression(expr.Operand, insideAggregate)
	case BinaryExpression:
		left, err := c.inspectSummarizeExpression(expr.Left, insideAggregate)
		if err != nil {
			return false, err
		}
		right, err := c.inspectSummarizeExpression(expr.Right, insideAggregate)
		return left || right, err
	case ListExpression:
		found := false
		for _, item := range expr.Items {
			contains, err := c.inspectSummarizeExpression(item, insideAggregate)
			if err != nil {
				return false, err
			}
			found = found || contains
		}
		return found, nil
	case FunctionExpression:
		isAggregate := expr.Name == "count" || expr.Name == "countif" || expr.Name == "dcount" || expr.Name == "sum" || expr.Name == "min" || expr.Name == "max" || expr.Name == "avg"
		if isAggregate && insideAggregate {
			return false, errorAt(expr.At, "aggregation functions cannot be nested")
		}
		found := isAggregate
		for _, argument := range expr.Arguments {
			contains, err := c.inspectSummarizeExpression(argument, insideAggregate || isAggregate)
			if err != nil {
				return false, err
			}
			found = found || contains
		}
		return found, nil
	default:
		return false, nil
	}
}

func quoteIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
func columnSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
func (c *compiler) bind(value any) string {
	c.args = append(c.args, value)
	return fmt.Sprintf("?%d", len(c.args))
}
