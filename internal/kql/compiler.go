package kql

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/kawijayaa/striem/internal/eventtime"
)

type CompiledQuery struct {
	SQL            string
	Args           []any
	Columns        []string
	DynamicColumns map[string]struct{}
}

type TableCatalog map[string]int64

type compiler struct {
	args      []any
	columns   map[string]struct{}
	dynamic   map[string]struct{}
	variables map[string]string
	constants map[string]any
	tabular   map[string]relation
	declared  map[string]struct{}
	tables    TableCatalog
	now       time.Time
}

type relation struct {
	SQL            string
	Columns        []string
	DynamicColumns map[string]struct{}
}

var eventColumns = []string{"TimeGenerated", "Source", "EventType", "Host", "User", "Message", "RawData"}

func Compile(query Query, now time.Time, catalogs ...TableCatalog) (CompiledQuery, error) {
	var tables TableCatalog
	if len(catalogs) > 0 {
		tables = catalogs[0]
	}
	c := &compiler{
		columns:   make(map[string]struct{}),
		dynamic:   make(map[string]struct{}),
		variables: make(map[string]string),
		constants: make(map[string]any),
		tabular:   make(map[string]relation),
		declared:  make(map[string]struct{}),
		tables:    tables,
		now:       now.UTC(),
	}
	eventColumnSet := columnSet(eventColumns)
	for _, binding := range query.Bindings {
		if _, exists := c.declared[binding.Name]; exists {
			return CompiledQuery{}, errorAt(binding.At, "variable %q is declared more than once", binding.Name)
		}
		if _, exists := eventColumnSet[binding.Name]; exists {
			return CompiledQuery{}, errorAt(binding.At, "variable %q conflicts with a table column", binding.Name)
		}
		c.columns = make(map[string]struct{})
		if binding.Tabular != nil {
			value, err := c.compilePipeline(*binding.Tabular)
			if err != nil {
				return CompiledQuery{}, err
			}
			c.tabular[binding.Name] = value
			c.declared[binding.Name] = struct{}{}
			continue
		}
		value, err := c.compileExpression(binding.Expression, false)
		if err != nil {
			return CompiledQuery{}, err
		}
		c.variables[binding.Name] = value
		c.declared[binding.Name] = struct{}{}
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
	return CompiledQuery{SQL: compiled.SQL, Args: c.args, Columns: compiled.Columns, DynamicColumns: compiled.DynamicColumns}, nil
}

func (c *compiler) compilePipeline(query Query) (relation, error) {
	previousColumns := c.columns
	previousDynamic := c.dynamic
	defer func() {
		c.columns = previousColumns
		c.dynamic = previousDynamic
	}()

	if binding, exists := c.tabular[query.Source]; exists {
		columns := append([]string(nil), binding.Columns...)
		c.columns = columnSet(columns)
		c.dynamic = cloneSet(binding.DynamicColumns)
		sqlText := projectRelation(binding, columns, "t")
		return c.compileOperators(sqlText, columns, cloneSet(binding.DynamicColumns), query.Operators)
	}
	datasetID, tableFound := c.tables[query.Source]
	if query.Source != "Events" && !tableFound {
		return relation{}, errorAt(query.SourceAt, "unknown table %q", query.Source)
	}
	c.columns = columnSet(eventColumns)
	c.dynamic = map[string]struct{}{"RawData": {}}
	sqlText := `SELECT time_generated AS "TimeGenerated", source AS "Source", event_type AS "EventType", host AS "Host", username AS "User", message AS "Message", raw_data AS "RawData" FROM events`
	if query.Source != "Events" {
		sqlText += " WHERE dataset_id = " + c.bind(datasetID)
	}
	columns := append([]string(nil), eventColumns...)
	return c.compileOperators(sqlText, columns, map[string]struct{}{"RawData": {}}, query.Operators)
}

func (c *compiler) compileOperators(sqlText string, columns []string, dynamicColumns map[string]struct{}, operators []Operator) (relation, error) {
	c.dynamic = dynamicColumns
	for _, rawOperator := range operators {
		switch operator := rawOperator.(type) {
		case WhereOperator:
			expr, err := c.compileExpression(operator.Expression, false)
			if err != nil {
				return relation{}, err
			}
			sqlText = fmt.Sprintf("SELECT * FROM (%s) AS q WHERE %s", sqlText, expr)
		case SearchOperator:
			needle, err := c.compileExpression(operator.Expression, false)
			if err != nil {
				return relation{}, err
			}
			conditions := make([]string, len(columns))
			for index, column := range columns {
				conditions[index] = "kql_has(COALESCE(CAST(q." + quoteIdentifier(column) + " AS TEXT), ''), COALESCE(CAST(" + needle + " AS TEXT), ''), 0)"
			}
			sqlText = fmt.Sprintf("SELECT * FROM (%s) AS q WHERE (%s)", sqlText, strings.Join(conditions, " OR "))
		case ProjectOperator:
			if len(operator.Items) > maxProjectionItems {
				return relation{}, errorAt(operator.At, "projection supports at most %d items", maxProjectionItems)
			}
			nextDynamic := make(map[string]struct{})
			for _, item := range operator.Items {
				if c.expressionIsDynamic(item.Expression, dynamicColumns) {
					nextDynamic[item.Name] = struct{}{}
				}
			}
			selects, names, err := c.compileNamed(operator.Items, false)
			if err != nil {
				return relation{}, err
			}
			sqlText = fmt.Sprintf("SELECT %s FROM (%s) AS q", strings.Join(selects, ", "), sqlText)
			columns, c.columns = names, columnSet(names)
			dynamicColumns = nextDynamic
			c.dynamic = dynamicColumns
		case ProjectAwayOperator:
			if len(operator.Patterns) > maxProjectionItems {
				return relation{}, errorAt(operator.At, "projection supports at most %d items", maxProjectionItems)
			}
			removed := make(map[string]struct{})
			for _, pattern := range operator.Patterns {
				matched := false
				for _, column := range columns {
					if columnPatternMatches(pattern, column) {
						removed[column] = struct{}{}
						matched = true
					}
				}
				if !matched && !pattern.PrefixWildcard && !pattern.SuffixWildcard {
					return relation{}, errorAt(pattern.At, "unknown column %q", pattern.Name)
				}
			}
			nextColumns := make([]string, 0, len(columns))
			selects := make([]string, 0, len(columns))
			nextDynamic := cloneSet(dynamicColumns)
			for _, column := range columns {
				if _, drop := removed[column]; drop {
					delete(nextDynamic, column)
					continue
				}
				nextColumns = append(nextColumns, column)
				selects = append(selects, "q."+quoteIdentifier(column)+" AS "+quoteIdentifier(column))
			}
			if len(nextColumns) == 0 {
				return relation{}, errorAt(operator.At, "project-away cannot remove every column")
			}
			sqlText = fmt.Sprintf("SELECT %s FROM (%s) AS q", strings.Join(selects, ", "), sqlText)
			columns, dynamicColumns = nextColumns, nextDynamic
			c.columns, c.dynamic = columnSet(columns), dynamicColumns
		case ProjectRenameOperator:
			if len(operator.Items) > maxProjectionItems {
				return relation{}, errorAt(operator.At, "projection supports at most %d items", maxProjectionItems)
			}
			renamed, err := c.compileProjectRename(relation{SQL: sqlText, Columns: columns, DynamicColumns: dynamicColumns}, operator)
			if err != nil {
				return relation{}, err
			}
			sqlText, columns, dynamicColumns = renamed.SQL, renamed.Columns, renamed.DynamicColumns
			c.columns, c.dynamic = columnSet(columns), dynamicColumns
		case ExtendOperator:
			inputNames := caseInsensitiveColumnSet(columns)
			for _, item := range operator.Items {
				if _, collision := inputNames[strings.ToLower(item.Name)]; collision {
					if _, replaces := c.columns[item.Name]; !replaces {
						return relation{}, errorAt(item.At, "column %q conflicts with another output column", item.Name)
					}
				}
			}
			nextDynamic := cloneSet(dynamicColumns)
			for _, item := range operator.Items {
				delete(nextDynamic, item.Name)
				if c.expressionIsDynamic(item.Expression, dynamicColumns) {
					nextDynamic[item.Name] = struct{}{}
				}
			}
			dynamicColumns = nextDynamic
			c.dynamic = dynamicColumns
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
			summarized, err := c.compileSummarize(relation{SQL: sqlText, Columns: columns, DynamicColumns: dynamicColumns}, operator)
			if err != nil {
				return relation{}, err
			}
			sqlText, columns, dynamicColumns = summarized.SQL, summarized.Columns, summarized.DynamicColumns
			c.columns = columnSet(columns)
			c.dynamic = dynamicColumns
		case DistinctOperator:
			selects, names, err := c.compileNamed(operator.Items, false)
			if err != nil {
				return relation{}, err
			}
			sqlText = fmt.Sprintf("SELECT DISTINCT %s FROM (%s) AS q", strings.Join(selects, ", "), sqlText)
			columns, c.columns = names, columnSet(names)
			nextDynamic := make(map[string]struct{})
			for _, item := range operator.Items {
				if c.expressionIsDynamic(item.Expression, dynamicColumns) {
					nextDynamic[item.Name] = struct{}{}
				}
			}
			dynamicColumns = nextDynamic
			c.dynamic = dynamicColumns
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
			dynamicColumns = make(map[string]struct{})
			c.dynamic = dynamicColumns
		case ParseOperator:
			parsed, err := c.compileParse(relation{SQL: sqlText, Columns: columns, DynamicColumns: dynamicColumns}, operator)
			if err != nil {
				return relation{}, err
			}
			sqlText, columns, dynamicColumns = parsed.SQL, parsed.Columns, parsed.DynamicColumns
			c.columns, c.dynamic = columnSet(columns), dynamicColumns
		case BagUnpackOperator:
			unpacked, err := c.compileBagUnpack(relation{SQL: sqlText, Columns: columns, DynamicColumns: dynamicColumns}, operator)
			if err != nil {
				return relation{}, err
			}
			sqlText, columns, dynamicColumns = unpacked.SQL, unpacked.Columns, unpacked.DynamicColumns
			c.columns, c.dynamic = columnSet(columns), dynamicColumns
		case MVExpandOperator:
			if _, collision := caseInsensitiveColumnSet(columns)[strings.ToLower(operator.Name)]; collision {
				if _, replaces := c.columns[operator.Name]; !replaces {
					return relation{}, errorAt(operator.At, "column %q conflicts with another output column", operator.Name)
				}
			}
			expression, err := c.compileExpression(operator.Expression, false)
			if err != nil {
				return relation{}, err
			}
			limit, err := c.evaluateRowLimit(operator.Limit)
			if err != nil {
				return relation{}, err
			}
			if limit > 128 {
				return relation{}, errorAt(operator.At, "mv-expand limit cannot exceed 128")
			}
			selects := make([]string, 0, len(columns)+1)
			replaced := false
			for _, column := range columns {
				if column == operator.Name {
					selects = append(selects, "mv.value AS "+quoteIdentifier(column))
					replaced = true
				} else {
					selects = append(selects, "q."+quoteIdentifier(column)+" AS "+quoteIdentifier(column))
				}
			}
			if !replaced {
				columns = append(columns, operator.Name)
				selects = append(selects, "mv.value AS "+quoteIdentifier(operator.Name))
			}
			delete(dynamicColumns, operator.Name)
			c.dynamic = dynamicColumns
			inputLimit, expansionLimit := c.bind(int64(1000)), c.bind(limit)
			sqlText = fmt.Sprintf(`SELECT %s FROM (SELECT * FROM (%s) LIMIT %s) AS q JOIN json_each(CASE WHEN json_valid(%s) THEN %s ELSE json_array(%s) END) AS mv ON (mv.key IS NULL OR CAST(mv.key AS INTEGER) < %s)`, strings.Join(selects, ", "), sqlText, inputLimit, expression, expression, expression, expansionLimit)
			c.columns = columnSet(columns)
			c.dynamic = dynamicColumns
		case MVApplyOperator:
			applied, err := c.compileMVApply(relation{SQL: sqlText, Columns: columns, DynamicColumns: dynamicColumns}, operator)
			if err != nil {
				return relation{}, err
			}
			sqlText, columns, dynamicColumns = applied.SQL, applied.Columns, applied.DynamicColumns
			c.columns = columnSet(columns)
			c.dynamic = dynamicColumns
		case UnionOperator:
			combined, err := c.compileUnion(relation{SQL: sqlText, Columns: columns, DynamicColumns: dynamicColumns}, operator)
			if err != nil {
				return relation{}, err
			}
			sqlText, columns, dynamicColumns = combined.SQL, combined.Columns, combined.DynamicColumns
			c.columns = columnSet(columns)
			c.dynamic = dynamicColumns
		case JoinOperator:
			joined, err := c.compileJoin(relation{SQL: sqlText, Columns: columns, DynamicColumns: dynamicColumns}, operator)
			if err != nil {
				return relation{}, err
			}
			sqlText, columns, dynamicColumns = joined.SQL, joined.Columns, joined.DynamicColumns
			c.columns = columnSet(columns)
			c.dynamic = dynamicColumns
		}
	}
	return relation{SQL: sqlText, Columns: columns, DynamicColumns: dynamicColumns}, nil
}

func columnPatternMatches(pattern ColumnPattern, column string) bool {
	if pattern.PrefixWildcard && pattern.SuffixWildcard {
		return strings.Contains(column, pattern.Name)
	}
	if pattern.PrefixWildcard {
		return strings.HasSuffix(column, pattern.Name)
	}
	if pattern.SuffixWildcard {
		return strings.HasPrefix(column, pattern.Name)
	}
	return column == pattern.Name
}

func (c *compiler) compileProjectRename(input relation, operator ProjectRenameOperator) (relation, error) {
	sources := make(map[string]ColumnRename, len(operator.Items))
	for _, item := range operator.Items {
		if _, exists := c.columns[item.Source]; !exists {
			return relation{}, errorAt(item.At, "unknown column %q", item.Source)
		}
		if _, duplicate := sources[item.Source]; duplicate {
			return relation{}, errorAt(item.At, "column %q is renamed more than once", item.Source)
		}
		sources[item.Source] = item
	}

	columns := make([]string, len(input.Columns))
	seen := make(map[string]struct{}, len(input.Columns))
	for index, source := range input.Columns {
		name := source
		at := operator.At
		if item, rename := sources[source]; rename {
			name, at = item.Name, item.At
		}
		key := strings.ToLower(name)
		if _, duplicate := seen[key]; duplicate {
			return relation{}, errorAt(at, "column %q conflicts with another output column", name)
		}
		seen[key] = struct{}{}
		columns[index] = name
	}

	selects := make([]string, len(input.Columns))
	dynamic := make(map[string]struct{})
	for index, source := range input.Columns {
		output := columns[index]
		selects[index] = "q." + quoteIdentifier(source) + " AS " + quoteIdentifier(output)
		if _, exists := input.DynamicColumns[source]; exists {
			dynamic[output] = struct{}{}
		}
	}
	return relation{SQL: fmt.Sprintf("SELECT %s FROM (%s) AS q", strings.Join(selects, ", "), input.SQL), Columns: columns, DynamicColumns: dynamic}, nil
}

func (c *compiler) compileParse(input relation, operator ParseOperator) (relation, error) {
	if len(operator.Pattern) == 0 {
		return relation{}, errorAt(operator.At, "parse pattern cannot be empty")
	}
	seen := caseInsensitiveColumnSet(input.Columns)
	captures := make([]ParsePatternItem, 0, maxParseCaptures)
	var pattern strings.Builder
	pattern.WriteString("(?s)^")
	for _, item := range operator.Pattern {
		switch item.Kind {
		case ParseDelimiter:
			pattern.WriteString(regexp.QuoteMeta(item.Text))
		case ParseWildcard:
			pattern.WriteString(".*?")
		case ParseCapture:
			key := strings.ToLower(item.Text)
			if _, collision := seen[key]; collision {
				return relation{}, errorAt(item.At, "column %q conflicts with another output column", item.Text)
			}
			seen[key] = struct{}{}
			captures = append(captures, item)
			switch item.Type {
			case ScalarString:
				pattern.WriteString("(.*?)")
			case ScalarLong:
				pattern.WriteString("([+-]?[0-9]+)")
			case ScalarReal:
				pattern.WriteString("([+-]?(?:[0-9]+(?:\\.[0-9]*)?|\\.[0-9]+)(?:[eE][+-]?[0-9]+)?)")
			default:
				return relation{}, errorAt(item.At, "parse capture type %q is not supported", item.Type)
			}
		}
	}
	pattern.WriteString("$")
	if len(captures) > maxParseCaptures {
		return relation{}, errorAt(operator.At, "parse supports at most %d captures", maxParseCaptures)
	}
	if pattern.Len() > maxParsePattern {
		return relation{}, errorAt(operator.At, "parse pattern cannot exceed %d bytes", maxParsePattern)
	}

	expression, err := c.compileExpression(operator.Expression, false)
	if err != nil {
		return relation{}, err
	}
	internal := availableCaseInsensitiveColumnName("__kql_parse_result", seen)
	types := make([]string, len(captures))
	for index, capture := range captures {
		types[index] = string(capture.Type)
	}
	parsed := "kql_parse(" + c.bind(pattern.String()) + ", " + c.bind(strings.Join(types, ",")) + ", CAST(" + expression + " AS TEXT))"
	inner := fmt.Sprintf("SELECT q.*, %s AS %s FROM (%s) AS q", parsed, quoteIdentifier(internal), input.SQL)
	guarded := "CASE WHEN json_valid(q." + quoteIdentifier(internal) + ") THEN q." + quoteIdentifier(internal) + " ELSE NULL END"

	selects := make([]string, 0, len(input.Columns)+len(captures))
	columns := append([]string(nil), input.Columns...)
	for _, column := range input.Columns {
		selects = append(selects, "q."+quoteIdentifier(column)+" AS "+quoteIdentifier(column))
	}
	for index, capture := range captures {
		value := fmt.Sprintf("json_extract(%s, '$[%d]')", guarded, index)
		switch capture.Type {
		case ScalarString:
			value = "CAST(" + value + " AS TEXT)"
		case ScalarLong:
			value = "CAST(" + value + " AS INTEGER)"
		case ScalarReal:
			value = "CAST(" + value + " AS REAL)"
		}
		selects = append(selects, value+" AS "+quoteIdentifier(capture.Text))
		columns = append(columns, capture.Text)
	}
	where := ""
	if operator.Where {
		where = " WHERE q." + quoteIdentifier(internal) + " IS NOT NULL"
	}
	return relation{SQL: fmt.Sprintf("SELECT %s FROM (%s) AS q%s", strings.Join(selects, ", "), inner, where), Columns: columns, DynamicColumns: cloneSet(input.DynamicColumns)}, nil
}

func (c *compiler) compileBagUnpack(input relation, operator BagUnpackOperator) (relation, error) {
	if _, exists := c.columns[operator.Column]; !exists {
		return relation{}, errorAt(operator.At, "unknown column %q", operator.Column)
	}
	if _, dynamic := input.DynamicColumns[operator.Column]; !dynamic {
		return relation{}, errorAt(operator.At, "bag_unpack requires a dynamic column")
	}
	if len(operator.Items) > maxBagUnpackItems {
		return relation{}, errorAt(operator.At, "bag_unpack supports at most %d output columns", maxBagUnpackItems)
	}

	preservedColumns := make([]string, 0, len(input.Columns)-1)
	for _, column := range input.Columns {
		if column != operator.Column {
			preservedColumns = append(preservedColumns, column)
		}
	}
	seen := caseInsensitiveColumnSet(preservedColumns)
	selects := make([]string, 0, len(input.Columns)+len(operator.Items))
	columns := append([]string(nil), preservedColumns...)
	dynamic := cloneSet(input.DynamicColumns)
	delete(dynamic, operator.Column)
	for _, column := range preservedColumns {
		selects = append(selects, "q."+quoteIdentifier(column)+" AS "+quoteIdentifier(column))
	}
	base := "q." + quoteIdentifier(operator.Column)
	guarded := "CASE WHEN json_valid(" + base + ") THEN " + base + " ELSE NULL END"
	for _, item := range operator.Items {
		key := strings.ToLower(item.Name)
		if _, collision := seen[key]; collision {
			return relation{}, errorAt(item.At, "column %q conflicts with another output column", item.Name)
		}
		seen[key] = struct{}{}
		property := item.Name
		if operator.Prefix != "" {
			if !strings.HasPrefix(item.Name, operator.Prefix) || len(item.Name) == len(operator.Prefix) {
				return relation{}, errorAt(item.At, "bag_unpack output column %q must start with prefix %q", item.Name, operator.Prefix)
			}
			property = strings.TrimPrefix(item.Name, operator.Prefix)
		}
		path := c.bind(`$."` + strings.ReplaceAll(property, `"`, `\"`) + `"`)
		value := "json_extract(" + guarded + ", " + path + ")"
		jsonType := "json_type(" + guarded + ", " + path + ")"
		switch item.Type {
		case ScalarString:
			value = "CASE WHEN " + jsonType + " = 'text' THEN " + value + " END"
		case ScalarLong:
			value = "CASE WHEN " + jsonType + " = 'integer' AND typeof(" + value + ") = 'integer' THEN " + value + " END"
		case ScalarReal:
			value = "CASE WHEN " + jsonType + " IN ('integer', 'real') AND abs(CAST(" + value + " AS REAL)) <= 1.7976931348623157e308 THEN CAST(" + value + " AS REAL) END"
		case ScalarDynamic:
			value = "CASE " + jsonType +
				" WHEN 'text' THEN json_quote(" + value + ")" +
				" WHEN 'object' THEN " + value +
				" WHEN 'array' THEN " + value +
				" WHEN 'true' THEN 'true'" +
				" WHEN 'false' THEN 'false'" +
				" WHEN 'integer' THEN " + value +
				" WHEN 'real' THEN CASE WHEN abs(CAST(" + value + " AS REAL)) <= 1.7976931348623157e308 THEN " + value + " END" +
				" ELSE NULL END"
			dynamic[item.Name] = struct{}{}
		default:
			return relation{}, errorAt(item.At, "bag_unpack output type %q is not supported", item.Type)
		}
		selects = append(selects, value+" AS "+quoteIdentifier(item.Name))
		columns = append(columns, item.Name)
	}
	return relation{SQL: fmt.Sprintf("SELECT %s FROM (%s) AS q", strings.Join(selects, ", "), input.SQL), Columns: columns, DynamicColumns: dynamic}, nil
}

func (c *compiler) compileMVApply(input relation, operator MVApplyOperator) (relation, error) {
	if len(operator.Aggregates) > 32 {
		return relation{}, errorAt(operator.At, "mv-apply supports at most 32 aggregates")
	}
	expressionDynamic := c.expressionIsDynamic(operator.Expression, input.DynamicColumns)
	expression, err := c.compileExpression(operator.Expression, false)
	if err != nil {
		return relation{}, err
	}
	limit, err := c.evaluateRowLimit(operator.Limit)
	if err != nil {
		return relation{}, err
	}
	if limit > 128 {
		return relation{}, errorAt(operator.At, "mv-apply limit cannot exceed 128")
	}

	inputColumns := make(map[string]struct{}, len(input.Columns))
	for _, column := range input.Columns {
		inputColumns[strings.ToLower(column)] = struct{}{}
	}
	seen := make(map[string]struct{}, len(operator.Aggregates))
	for _, item := range operator.Aggregates {
		name := strings.ToLower(item.Name)
		if _, exists := inputColumns[name]; exists {
			return relation{}, errorAt(item.At, "mv-apply aggregate column %q conflicts with an input column", item.Name)
		}
		if _, exists := seen[name]; exists {
			return relation{}, errorAt(item.At, "column %q is specified more than once", item.Name)
		}
		seen[name] = struct{}{}
		if function, found := findArgExtremum(item.Expression); found {
			return relation{}, errorAt(function.At, "%s() is not supported in mv-apply", function.Name)
		}
		if err := c.validateSummarizeExpression(item.Expression); err != nil {
			return relation{}, err
		}
	}

	c.columns = map[string]struct{}{operator.Alias: {}}
	c.dynamic = make(map[string]struct{})
	if expressionDynamic {
		c.dynamic[operator.Alias] = struct{}{}
	}
	conditions := make([]string, len(operator.Wheres))
	for index, where := range operator.Wheres {
		conditions[index], err = c.compileExpression(where, false)
		if err != nil {
			return relation{}, err
		}
	}

	expansionLimit := c.bind(limit)
	expanded := fmt.Sprintf(`SELECT mv.value AS %s FROM json_each(CASE WHEN json_valid(%s) THEN %s ELSE json_array(%s) END) AS mv LIMIT %s`, quoteIdentifier(operator.Alias), expression, expression, expression, expansionLimit)
	whereSQL := ""
	if len(conditions) > 0 {
		whereSQL = " WHERE " + strings.Join(conditions, " AND ")
	}

	selects := make([]string, 0, len(input.Columns)+len(operator.Aggregates))
	for _, column := range input.Columns {
		selects = append(selects, "q."+quoteIdentifier(column)+" AS "+quoteIdentifier(column))
	}
	columns := append([]string(nil), input.Columns...)
	dynamic := cloneSet(input.DynamicColumns)
	for _, item := range operator.Aggregates {
		aggregate, compileErr := c.compileExpression(item.Expression, true)
		if compileErr != nil {
			return relation{}, compileErr
		}
		selects = append(selects, fmt.Sprintf("(SELECT %s FROM (%s) AS q%s) AS %s", aggregate, expanded, whereSQL, quoteIdentifier(item.Name)))
		columns = append(columns, item.Name)
		if c.expressionIsDynamic(item.Expression, c.dynamic) {
			dynamic[item.Name] = struct{}{}
		}
	}

	inputLimit := c.bind(int64(1000))
	sqlText := fmt.Sprintf("SELECT %s FROM (SELECT * FROM (%s) LIMIT %s) AS q", strings.Join(selects, ", "), input.SQL, inputLimit)
	return relation{SQL: sqlText, Columns: columns, DynamicColumns: dynamic}, nil
}

func findArgExtremum(expression Expression) (FunctionExpression, bool) {
	switch expr := expression.(type) {
	case UnaryExpression:
		return findArgExtremum(expr.Operand)
	case BinaryExpression:
		if function, found := findArgExtremum(expr.Left); found {
			return function, true
		}
		return findArgExtremum(expr.Right)
	case ListExpression:
		for _, item := range expr.Items {
			if function, found := findArgExtremum(item); found {
				return function, true
			}
		}
	case FunctionExpression:
		if expr.Name == "arg_max" || expr.Name == "arg_min" {
			return expr, true
		}
		for _, argument := range expr.Arguments {
			if function, found := findArgExtremum(argument); found {
				return function, true
			}
		}
	}
	return FunctionExpression{}, false
}

func (c *compiler) compileUnion(left relation, operator UnionOperator) (relation, error) {
	arms := []string{projectRelation(left, left.Columns, "u0")}
	leftSet := columnSet(left.Columns)
	dynamic := cloneSet(left.DynamicColumns)
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
		for name := range dynamic {
			if _, exists := right.DynamicColumns[name]; !exists {
				delete(dynamic, name)
			}
		}
		arms = append(arms, projectRelation(right, left.Columns, fmt.Sprintf("u%d", index+1)))
	}
	return relation{SQL: strings.Join(arms, " UNION ALL "), Columns: append([]string(nil), left.Columns...), DynamicColumns: dynamic}, nil
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
	if operator.Kind == JoinLeftAnti {
		selects := make([]string, len(left.Columns))
		for index, name := range left.Columns {
			selects[index] = "l." + quoteIdentifier(name) + " AS " + quoteIdentifier(name)
		}
		sqlText := fmt.Sprintf("SELECT %s FROM (%s) AS l WHERE NOT EXISTS (SELECT 1 FROM (%s) AS r WHERE %s)", strings.Join(selects, ", "), left.SQL, right.SQL, strings.Join(conditions, " AND "))
		return relation{SQL: sqlText, Columns: append([]string(nil), left.Columns...), DynamicColumns: cloneSet(left.DynamicColumns)}, nil
	}
	selects := make([]string, 0, len(left.Columns)+len(right.Columns))
	columns := append([]string(nil), left.Columns...)
	used := columnSet(columns)
	dynamic := cloneSet(left.DynamicColumns)
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
		if _, isDynamic := right.DynamicColumns[name]; isDynamic {
			dynamic[output] = struct{}{}
		}
	}
	joinSQL := "INNER JOIN"
	if operator.Kind == JoinLeftOuter {
		joinSQL = "LEFT OUTER JOIN"
	}
	sqlText := fmt.Sprintf("SELECT %s FROM (%s) AS l %s (%s) AS r ON %s", strings.Join(selects, ", "), left.SQL, joinSQL, right.SQL, strings.Join(conditions, " AND "))
	return relation{SQL: sqlText, Columns: columns, DynamicColumns: dynamic}, nil
}

func (c *compiler) compileSummarize(input relation, operator SummarizeOperator) (relation, error) {
	for _, item := range operator.Aggregates {
		if err := c.validateSummarizeExpression(item.Expression); err != nil {
			return relation{}, err
		}
	}
	for _, item := range operator.Aggregates {
		function, ok := item.Expression.(FunctionExpression)
		if !ok || (function.Name != "arg_max" && function.Name != "arg_min") {
			continue
		}
		if len(operator.Aggregates) != 1 {
			return relation{}, errorAt(function.At, "%s(..., *) cannot be combined with other aggregations", function.Name)
		}
		if item.Explicit {
			return relation{}, errorAt(item.At, "%s(..., *) cannot have a column alias", function.Name)
		}
		return c.compileArgExtremum(input, operator.Groups, function)
	}

	aggregates, aggregateNames, err := c.compileNamed(operator.Aggregates, true)
	if err != nil {
		return relation{}, err
	}
	groups, groupNames, err := c.compileNamed(operator.Groups, false)
	if err != nil {
		return relation{}, err
	}
	outputNames := make(map[string]struct{}, len(groupNames)+len(aggregateNames))
	for _, item := range append(append([]NamedExpression(nil), operator.Groups...), operator.Aggregates...) {
		key := strings.ToLower(item.Name)
		if _, duplicate := outputNames[key]; duplicate {
			return relation{}, errorAt(item.At, "column %q is specified more than once", item.Name)
		}
		outputNames[key] = struct{}{}
	}
	selects := append(groups, aggregates...)
	sqlText := fmt.Sprintf("SELECT %s FROM (%s) AS q", strings.Join(selects, ", "), input.SQL)
	if len(groups) > 0 {
		positions := make([]string, len(groups))
		for index := range groups {
			positions[index] = fmt.Sprintf("%d", index+1)
		}
		sqlText += " GROUP BY " + strings.Join(positions, ", ")
	}
	columns := append(groupNames, aggregateNames...)
	dynamic := make(map[string]struct{})
	for _, item := range append(append([]NamedExpression(nil), operator.Groups...), operator.Aggregates...) {
		if c.expressionIsDynamic(item.Expression, input.DynamicColumns) {
			dynamic[item.Name] = struct{}{}
		}
	}
	return relation{SQL: sqlText, Columns: columns, DynamicColumns: dynamic}, nil
}

func (c *compiler) compileArgExtremum(input relation, groups []NamedExpression, function FunctionExpression) (relation, error) {
	if len(function.Arguments) != 2 {
		return relation{}, errorAt(function.At, "%s() expects a value and '*'", function.Name)
	}
	if _, ok := function.Arguments[1].(StarExpression); !ok {
		return relation{}, errorAt(function.At, "%s() currently requires '*' as its second argument", function.Name)
	}
	key, err := c.compileExpression(function.Arguments[0], false)
	if err != nil {
		return relation{}, err
	}
	groupExpressions := make([]string, len(groups))
	groupNames := make([]string, len(groups))
	groupInternalNames := make([]string, len(groups))
	rankedSelects := make([]string, 0, len(groups)+2)
	used := make(map[string]struct{})
	internalNames := columnSet(input.Columns)
	for index, group := range groups {
		nameKey := strings.ToLower(group.Name)
		if _, exists := used[nameKey]; exists {
			return relation{}, errorAt(group.At, "column %q is specified more than once", group.Name)
		}
		used[nameKey] = struct{}{}
		expression, compileErr := c.compileExpression(group.Expression, false)
		if compileErr != nil {
			return relation{}, compileErr
		}
		groupExpressions[index], groupNames[index] = expression, group.Name
		internal := availableColumnName(fmt.Sprintf("__kql_group_%d", index), internalNames)
		internalNames[internal] = struct{}{}
		groupInternalNames[index] = internal
		rankedSelects = append(rankedSelects, expression+" AS "+quoteIdentifier(internal))
	}
	rankedSelects = append(rankedSelects, "q.*")
	direction := "DESC"
	if function.Name == "arg_min" {
		direction = "ASC"
	}
	window := "ROW_NUMBER() OVER ("
	if len(groupExpressions) > 0 {
		window += "PARTITION BY " + strings.Join(groupExpressions, ", ") + " "
	}
	rankName := availableColumnName("__kql_rank", internalNames)
	window += "ORDER BY (" + key + " IS NULL) ASC, " + key + " " + direction + ") AS " + quoteIdentifier(rankName)
	rankedSelects = append(rankedSelects, window)

	selects := make([]string, 0, len(groups)+len(input.Columns))
	columns := append([]string(nil), groupNames...)
	dynamic := make(map[string]struct{})
	for index, group := range groups {
		selects = append(selects, "r."+quoteIdentifier(groupInternalNames[index])+" AS "+quoteIdentifier(group.Name))
		if c.expressionIsDynamic(group.Expression, input.DynamicColumns) {
			dynamic[group.Name] = struct{}{}
		}
	}
	for _, column := range input.Columns {
		nameKey := strings.ToLower(column)
		if _, duplicate := used[nameKey]; duplicate {
			continue
		}
		used[nameKey] = struct{}{}
		columns = append(columns, column)
		selects = append(selects, "r."+quoteIdentifier(column)+" AS "+quoteIdentifier(column))
		if _, isDynamic := input.DynamicColumns[column]; isDynamic {
			dynamic[column] = struct{}{}
		}
	}
	ranked := fmt.Sprintf("SELECT %s FROM (%s) AS q", strings.Join(rankedSelects, ", "), input.SQL)
	sqlText := fmt.Sprintf("SELECT %s FROM (%s) AS r WHERE r.%s = 1", strings.Join(selects, ", "), ranked, quoteIdentifier(rankName))
	return relation{SQL: sqlText, Columns: columns, DynamicColumns: dynamic}, nil
}

func projectRelation(value relation, columns []string, alias string) string {
	selects := make([]string, len(columns))
	for index, name := range columns {
		selects[index] = alias + "." + quoteIdentifier(name) + " AS " + quoteIdentifier(name)
	}
	return fmt.Sprintf("SELECT %s FROM (%s) AS %s", strings.Join(selects, ", "), value.SQL, alias)
}

func availableColumnName(name string, used map[string]struct{}) string {
	contains := func(candidate string) bool {
		for existing := range used {
			if strings.EqualFold(existing, candidate) {
				return true
			}
		}
		return false
	}
	if !contains(name) {
		return name
	}
	for suffix := 1; suffix <= 1000; suffix++ {
		candidate := fmt.Sprintf("%s%d", name, suffix)
		if !contains(candidate) {
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
		key := strings.ToLower(item.Name)
		if _, exists := seen[key]; exists {
			return nil, nil, errorAt(item.At, "column %q is specified more than once", item.Name)
		}
		seen[key] = struct{}{}
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
		for index, part := range expr.Parts[1:] {
			if arrayIndex, indexed := expr.Indices[index+1]; indexed {
				path += fmt.Sprintf("[%d]", arrayIndex)
			} else {
				path += `."` + strings.ReplaceAll(part, `"`, `\"`) + `"`
			}
		}
		return `json_extract(CASE WHEN json_valid(` + base + `) THEN ` + base + ` ELSE NULL END, ` + c.bind(path) + `)`, nil
	case LiteralExpression:
		return c.bind(expr.Value), nil
	case DurationExpression:
		return "", errorAt(expr.At, "duration literals are only valid in ago() and bin()")
	case StarExpression:
		return "", errorAt(expr.At, "'*' is only supported as the second argument to arg_max() or arg_min()")
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
		if expr.Operator == "in" || expr.Operator == "in~" || expr.Operator == "!in" || expr.Operator == "!in~" {
			list := expr.Right.(ListExpression)
			values := make([]string, 0, len(list.Items))
			for _, item := range list.Items {
				value, itemErr := c.compileExpression(item, aggregate)
				if itemErr != nil {
					return "", itemErr
				}
				values = append(values, value)
			}
			caseInsensitive := expr.Operator == "in~" || expr.Operator == "!in~"
			if caseInsensitive {
				for index := range values {
					values[index] = "lower(CAST(" + values[index] + " AS TEXT))"
				}
				left = "lower(CAST(" + left + " AS TEXT))"
			}
			negated := ""
			if expr.Operator == "!in" || expr.Operator == "!in~" {
				negated = " NOT"
			}
			return fmt.Sprintf("(%s%s IN (%s))", left, negated, strings.Join(values, ", ")), nil
		}
		if expr.Operator == "has_any" || expr.Operator == "has_all" {
			list := expr.Right.(ListExpression)
			conditions := make([]string, 0, len(list.Items))
			for _, item := range list.Items {
				value, itemErr := c.compileExpression(item, aggregate)
				if itemErr != nil {
					return "", itemErr
				}
				conditions = append(conditions, "kql_has(CAST("+left+" AS TEXT), CAST("+value+" AS TEXT), 0)")
			}
			separator := " OR "
			if expr.Operator == "has_all" {
				separator = " AND "
			}
			return "(" + strings.Join(conditions, separator) + ")", nil
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
		case "=~", "!~":
			op := "="
			if expr.Operator == "!~" {
				op = "!="
			}
			return "(lower(CAST(" + left + " AS TEXT)) " + op + " lower(CAST(" + right + " AS TEXT)))", nil
		case "+", "-", "*", "/", "%":
			return "(" + left + " " + expr.Operator + " " + right + ")", nil
		case "contains":
			return "(instr(lower(CAST(" + left + " AS TEXT)), lower(CAST(" + right + " AS TEXT))) > 0)", nil
		case "startswith":
			return "(substr(lower(CAST(" + left + " AS TEXT)), 1, length(CAST(" + right + " AS TEXT))) = lower(CAST(" + right + " AS TEXT)))", nil
		case "endswith":
			return "(substr(lower(CAST(" + left + " AS TEXT)), -length(CAST(" + right + " AS TEXT))) = lower(CAST(" + right + " AS TEXT)))", nil
		case "contains_cs":
			return "(instr(CAST(" + left + " AS TEXT), CAST(" + right + " AS TEXT)) > 0)", nil
		case "startswith_cs":
			return "(substr(CAST(" + left + " AS TEXT), 1, length(CAST(" + right + " AS TEXT))) = CAST(" + right + " AS TEXT))", nil
		case "endswith_cs":
			return "(substr(CAST(" + left + " AS TEXT), -length(CAST(" + right + " AS TEXT))) = CAST(" + right + " AS TEXT))", nil
		case "has", "has_cs":
			caseSensitive := 0
			if expr.Operator == "has_cs" {
				caseSensitive = 1
			}
			return fmt.Sprintf("kql_has(CAST(%s AS TEXT), CAST(%s AS TEXT), %d)", left, right, caseSensitive), nil
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
			value, err := c.compileExpression(argument, aggregate)
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
		value, err := c.compileExpression(expr.Arguments[0], aggregate)
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
	case "tostring", "toint", "tolower", "toupper", "isnull", "isnotnull", "parse_json", "strlen", "array_length", "bag_keys", "isempty", "isnotempty", "todatetime":
		args, err := compileArgs(1)
		if err != nil {
			return "", err
		}
		switch expr.Name {
		case "tostring":
			if c.expressionIsDynamic(expr.Arguments[0], c.dynamic) {
				return "CASE WHEN json_valid(" + args[0] + ") AND json_type(" + args[0] + ") = 'text' THEN json_extract(" + args[0] + ", '$') ELSE CAST(" + args[0] + " AS TEXT) END", nil
			}
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
		case "array_length":
			return "kql_array_length(" + args[0] + ")", nil
		case "bag_keys":
			return "kql_bag_keys(" + args[0] + ")", nil
		case "isempty":
			if c.expressionIsDynamic(expr.Arguments[0], c.dynamic) {
				return "(" + args[0] + " IS NULL OR CASE WHEN json_valid(" + args[0] + ") AND json_type(" + args[0] + ") = 'text' THEN length(json_extract(" + args[0] + ", '$')) = 0 ELSE length(CAST(" + args[0] + " AS TEXT)) = 0 END)", nil
			}
			return "(COALESCE(length(CAST(" + args[0] + " AS TEXT)), 0) = 0)", nil
		case "isnotempty":
			if c.expressionIsDynamic(expr.Arguments[0], c.dynamic) {
				return "(" + args[0] + " IS NOT NULL AND CASE WHEN json_valid(" + args[0] + ") AND json_type(" + args[0] + ") = 'text' THEN length(json_extract(" + args[0] + ", '$')) > 0 ELSE length(CAST(" + args[0] + " AS TEXT)) > 0 END)", nil
			}
			return "(COALESCE(length(CAST(" + args[0] + " AS TEXT)), 0) > 0)", nil
		case "todatetime":
			return "kql_todatetime(" + args[0] + ")", nil
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
			value, err := c.compileExpression(argument, aggregate)
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
			value, err := c.compileExpression(argument, aggregate)
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
	case "split":
		args, err := compileArgs(2)
		if err != nil {
			return "", err
		}
		return "kql_split(CAST(" + args[0] + " AS TEXT), CAST(" + args[1] + " AS TEXT))", nil
	case "extract":
		args, err := compileArgs(3)
		if err != nil {
			return "", err
		}
		return "NULLIF(kql_extract(CAST(" + args[0] + " AS TEXT), CAST(" + args[1] + " AS INTEGER), CAST(" + args[2] + " AS TEXT)), '')", nil
	case "trim":
		args, err := compileArgs(2)
		if err != nil {
			return "", err
		}
		return "kql_trim(CAST(" + args[0] + " AS TEXT), CAST(" + args[1] + " AS TEXT))", nil
	case "replace_string":
		args, err := compileArgs(3)
		if err != nil {
			return "", err
		}
		return "replace(CAST(" + args[0] + " AS TEXT), CAST(" + args[1] + " AS TEXT), CAST(" + args[2] + " AS TEXT))", nil
	case "strcat":
		if len(expr.Arguments) < 2 {
			return "", errorAt(expr.At, "strcat() expects at least 2 arguments")
		}
		args := make([]string, len(expr.Arguments))
		for index, argument := range expr.Arguments {
			value, err := c.compileExpression(argument, aggregate)
			if err != nil {
				return "", err
			}
			args[index] = "COALESCE(CAST(" + value + " AS TEXT), '')"
		}
		return "(" + strings.Join(args, " || ") + ")", nil
	case "count", "dcount", "sum", "min", "max", "avg", "countif", "make_set", "make_list", "take_any":
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
			return "COALESCE(SUM(CASE WHEN " + args[0] + " THEN 1 ELSE 0 END), 0)", nil
		case "make_set":
			dynamic := 0
			if c.expressionIsDynamic(expr.Arguments[0], c.dynamic) {
				dynamic = 1
			}
			return fmt.Sprintf("kql_make_set(%s, %d)", args[0], dynamic), nil
		case "make_list":
			dynamic := 0
			if c.expressionIsDynamic(expr.Arguments[0], c.dynamic) {
				dynamic = 1
			}
			return fmt.Sprintf("kql_make_list(%s, %d)", args[0], dynamic), nil
		case "take_any":
			return "COALESCE(MIN(CASE WHEN typeof(" + args[0] + ") <> 'text' OR " + args[0] + " <> '' THEN " + args[0] + " END), MIN(" + args[0] + "))", nil
		default:
			return strings.ToUpper(expr.Name) + "(" + args[0] + ")", nil
		}
	case "arg_max", "arg_min":
		return "", errorAt(expr.At, "%s(..., *) must be a top-level summarize aggregation", expr.Name)
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
		isAggregate := isAggregateFunction(expr.Name)
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

func isAggregateFunction(name string) bool {
	switch name {
	case "count", "countif", "dcount", "sum", "min", "max", "avg", "make_set", "make_list", "take_any", "arg_max", "arg_min":
		return true
	default:
		return false
	}
}

func (c *compiler) expressionIsDynamic(expression Expression, dynamicColumns map[string]struct{}) bool {
	switch expr := expression.(type) {
	case IdentifierExpression:
		_, exists := dynamicColumns[expr.Parts[0]]
		return exists
	case FunctionExpression:
		switch expr.Name {
		case "make_set", "make_list", "parse_json", "split", "bag_keys":
			return true
		case "take_any":
			return len(expr.Arguments) == 1 && c.expressionIsDynamic(expr.Arguments[0], dynamicColumns)
		}
	}
	return false
}

func quoteIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
func columnSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
func caseInsensitiveColumnSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[strings.ToLower(value)] = struct{}{}
	}
	return result
}
func availableCaseInsensitiveColumnName(name string, used map[string]struct{}) string {
	if _, exists := used[strings.ToLower(name)]; !exists {
		return name
	}
	for suffix := 1; suffix <= 1000; suffix++ {
		candidate := fmt.Sprintf("%s%d", name, suffix)
		if _, exists := used[strings.ToLower(candidate)]; !exists {
			return candidate
		}
	}
	panic("too many column name collisions")
}
func cloneSet(values map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for value := range values {
		result[value] = struct{}{}
	}
	return result
}
func (c *compiler) bind(value any) string {
	c.args = append(c.args, value)
	return fmt.Sprintf("?%d", len(c.args))
}
