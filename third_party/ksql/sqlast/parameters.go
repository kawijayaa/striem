package sqlast

import "reflect"

// NormalizeParameters renumbers each parameter occurrence in SQL rendering
// order and returns the correspondingly ordered argument list. Reused
// parameter nodes are copied per occurrence, which is required by dialects
// whose anonymous placeholders cannot refer to an earlier argument.
func NormalizeParameters(query Query, arguments []any) []any {
	// Let/function expansion can share entire parent nodes, not just parameter
	// leaves. Give every rendered occurrence its own mutable tree so visiting
	// one occurrence cannot change a later occurrence's original argument index.
	cloneParameterOccurrences(query)

	result := make([]any, 0, len(arguments))
	var expression func(Expr) Expr
	var source func(Source)
	var visitQuery func(Query)

	expression = func(expr Expr) Expr {
		switch value := expr.(type) {
		case *Parameter:
			if value.Index <= 0 || value.Index > len(arguments) {
				return value
			}
			result = append(result, arguments[value.Index-1])
			return &Parameter{Index: len(result), Kind: value.Kind}
		case *Unary:
			value.Operand = expression(value.Operand)
		case *Binary:
			value.Left, value.Right = expression(value.Left), expression(value.Right)
		case *Call:
			for i := range value.Args {
				value.Args[i] = expression(value.Args[i])
			}
		case *Window:
			value.Expr = expression(value.Expr)
			for i := range value.PartitionBy {
				value.PartitionBy[i] = expression(value.PartitionBy[i])
			}
			for i := range value.OrderBy {
				value.OrderBy[i].Expr = expression(value.OrderBy[i].Expr)
			}
		case *List:
			for i := range value.Items {
				value.Items[i] = expression(value.Items[i])
			}
		case *Case:
			for i := range value.Branches {
				value.Branches[i].Condition = expression(value.Branches[i].Condition)
				value.Branches[i].Result = expression(value.Branches[i].Result)
			}
			value.Else = expression(value.Else)
		case *Cast:
			value.Expr = expression(value.Expr)
		case *Index:
			value.Value, value.Index = expression(value.Value), expression(value.Index)
		case *Regex:
			value.Value, value.Pattern = expression(value.Value), expression(value.Pattern)
		case *Exists:
			visitQuery(value.Query)
		case *RankedValue:
			value.Value = expression(value.Value)
		case *Filtered:
			value.Expr, value.Where = expression(value.Expr), expression(value.Where)
		}
		return expr
	}

	source = func(from Source) {
		switch value := from.(type) {
		case *Subquery:
			visitQuery(value.Query)
		case *Join:
			source(value.Left)
			source(value.Right)
			value.On = expression(value.On)
		case *Values:
			for i := range value.Rows {
				for j := range value.Rows[i] {
					value.Rows[i][j] = expression(value.Rows[i][j])
				}
			}
		case *Series:
			value.From, value.To = expression(value.From), expression(value.To)
			value.Step = expression(value.Step)
		case *JSONEach:
			value.Value = expression(value.Value)
		}
	}

	visitQuery = func(query Query) {
		switch value := query.(type) {
		case *With:
			for _, binding := range value.Bindings {
				visitQuery(binding.Query)
			}
			visitQuery(value.Query)
		case *Select:
			for i := range value.Projections {
				value.Projections[i].Expr = expression(value.Projections[i].Expr)
			}
			source(value.From)
			value.Where = expression(value.Where)
			for i := range value.GroupBy {
				value.GroupBy[i] = expression(value.GroupBy[i])
			}
			for i := range value.OrderBy {
				value.OrderBy[i].Expr = expression(value.OrderBy[i].Expr)
			}
			value.Limit = expression(value.Limit)
		case *Union:
			for _, operand := range value.Queries {
				visitQuery(operand)
			}
		}
	}
	visitQuery(query)
	return result
}

// SQL AST fields contain pointer nodes, structs, slices and scalar values.
// Clone per occurrence (without pointer memoization): repeated SQL syntax must
// receive independent parameter numbers, including across materialized bindings.
func cloneParameterOccurrences(query Query) {
	if query == nil {
		return
	}
	var clone func(reflect.Value) reflect.Value
	clone = func(value reflect.Value) reflect.Value {
		switch value.Kind() {
		case reflect.Pointer, reflect.Interface:
			if value.IsNil() {
				return reflect.Zero(value.Type())
			}
			child := clone(value.Elem())
			if value.Kind() == reflect.Pointer {
				result := reflect.New(value.Type().Elem())
				result.Elem().Set(child)
				return result
			}
			result := reflect.New(value.Type()).Elem()
			result.Set(child)
			return result
		case reflect.Struct:
			result := reflect.New(value.Type()).Elem()
			for i := 0; i < value.NumField(); i++ {
				result.Field(i).Set(clone(value.Field(i)))
			}
			return result
		case reflect.Slice:
			if value.IsNil() {
				return reflect.Zero(value.Type())
			}
			result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
			for i := 0; i < value.Len(); i++ {
				result.Index(i).Set(clone(value.Index(i)))
			}
			return result
		default:
			return value
		}
	}
	root := reflect.ValueOf(query).Elem()
	root.Set(clone(root))
}
