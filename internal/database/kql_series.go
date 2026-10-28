package database

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Series values are JSON arrays. Elements are numbers (kept integral when
// every operand is integral), or null for non-numeric and missing elements.

const maxSeriesElements = 1 << 20

type seriesValue struct {
	number  float64
	integer int64
	isInt   bool
	valid   bool
}

func parseSeries(value any) ([]seriesValue, bool) {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) {
		return nil, false
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var raw []any
	if err := decoder.Decode(&raw); err != nil || len(raw) > maxSeriesElements {
		return nil, false
	}
	series := make([]seriesValue, len(raw))
	for i, element := range raw {
		series[i] = seriesElement(element)
	}
	return series, true
}

func seriesElement(element any) seriesValue {
	number, ok := element.(json.Number)
	if !ok {
		return seriesValue{}
	}
	if integer, err := number.Int64(); err == nil && !strings.ContainsAny(number.String(), ".eE") {
		return seriesValue{number: float64(integer), integer: integer, isInt: true, valid: true}
	}
	real, err := number.Float64()
	if err != nil || math.IsNaN(real) || math.IsInf(real, 0) {
		return seriesValue{}
	}
	return seriesValue{number: real, valid: true}
}

// seriesOperand reads an array, or broadcasts a scalar number.
func seriesOperand(value any) ([]seriesValue, bool, bool) {
	switch number := value.(type) {
	case int64:
		return []seriesValue{{number: float64(number), integer: number, isInt: true, valid: true}}, true, true
	case float64:
		return []seriesValue{{number: number, valid: !math.IsNaN(number) && !math.IsInf(number, 0)}}, true, true
	}
	series, ok := parseSeries(value)
	return series, false, ok
}

func encodeSeries(values []any) (any, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

func finiteJSON(value float64) any {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return value
}

// kqlSeriesBinary applies an element-wise arithmetic or comparison operator.
func kqlSeriesBinary(operation string, left, right any) (any, error) {
	a, scalarA, okA := seriesOperand(left)
	b, scalarB, okB := seriesOperand(right)
	if !okA || !okB {
		return nil, nil
	}
	length := len(a)
	if scalarA {
		length = len(b)
	} else if !scalarB && len(b) > length {
		length = len(b)
	}
	at := func(series []seriesValue, scalar bool, index int) seriesValue {
		if scalar {
			return series[0]
		}
		if index < len(series) {
			return series[index]
		}
		return seriesValue{}
	}
	result := make([]any, length)
	for i := range result {
		x, y := at(a, scalarA, i), at(b, scalarB, i)
		if !x.valid || !y.valid {
			continue
		}
		integral := x.isInt && y.isInt
		switch operation {
		case "add":
			if integral && !overflowsAdd(x.integer, y.integer) {
				result[i] = x.integer + y.integer
			} else {
				result[i] = finiteJSON(x.number + y.number)
			}
		case "subtract":
			if integral && !overflowsAdd(x.integer, -y.integer) && y.integer != math.MinInt64 {
				result[i] = x.integer - y.integer
			} else {
				result[i] = finiteJSON(x.number - y.number)
			}
		case "multiply":
			product := x.number * y.number
			if integral && math.Abs(product) < 9e18 {
				result[i] = x.integer * y.integer
			} else {
				result[i] = finiteJSON(product)
			}
		case "divide":
			if y.number != 0 {
				result[i] = finiteJSON(x.number / y.number)
			}
		case "pow":
			result[i] = finiteJSON(math.Pow(x.number, y.number))
		case "greater":
			result[i] = x.number > y.number
		case "greater_equals":
			result[i] = x.number >= y.number
		case "less":
			result[i] = x.number < y.number
		case "less_equals":
			result[i] = x.number <= y.number
		case "equals":
			result[i] = x.number == y.number
		case "not_equals":
			result[i] = x.number != y.number
		default:
			return nil, fmt.Errorf("unknown series operation %s", operation)
		}
	}
	return encodeSeries(result)
}

func overflowsAdd(a, b int64) bool {
	sum := a + b
	return (a > 0 && b > 0 && sum < 0) || (a < 0 && b < 0 && sum >= 0)
}

var seriesUnary = map[string]func(float64) float64{
	"abs": math.Abs, "acos": math.Acos, "asin": math.Asin, "atan": math.Atan, "ceiling": math.Ceil,
	"cos": math.Cos, "exp": math.Exp, "floor": math.Floor, "log": math.Log, "sin": math.Sin,
	"sqrt": math.Sqrt, "tan": math.Tan,
	"sign": func(x float64) float64 {
		switch {
		case x > 0:
			return 1
		case x < 0:
			return -1
		}
		return 0
	},
}

func kqlSeriesUnary(operation string, value any) (any, error) {
	series, ok := parseSeries(value)
	function, known := seriesUnary[operation]
	if !ok || !known {
		return nil, nil
	}
	result := make([]any, len(series))
	for i, element := range series {
		if !element.valid {
			continue
		}
		if element.isInt && (operation == "abs" || operation == "sign" || operation == "floor" || operation == "ceiling") && element.integer != math.MinInt64 {
			result[i] = int64(function(element.number))
			continue
		}
		result[i] = finiteJSON(function(element.number))
	}
	return encodeSeries(result)
}

// seriesMissing reports whether an element is the missing-value placeholder:
// null by default, or a given number.
func seriesMissing(element seriesValue, placeholder any) bool {
	if placeholder == nil || sqliteNull(placeholder) {
		return !element.valid
	}
	value, _, ok := seriesOperand(placeholder)
	return ok && len(value) > 0 && element.valid && value[0].valid && element.number == value[0].number
}

func seriesJSON(element seriesValue) any {
	if !element.valid {
		return nil
	}
	if element.isInt {
		return element.integer
	}
	return element.number
}

// kqlSeriesFill implements series_fill_const/forward/backward/linear.
func kqlSeriesFill(kind string, values ...any) (any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	series, ok := parseSeries(values[0])
	if !ok {
		return nil, nil
	}
	argument := func(i int) any {
		if i < len(values) {
			return values[i]
		}
		return nil
	}
	result := make([]any, len(series))
	switch kind {
	case "const":
		constant := argument(1)
		var fill any = int64(0)
		if constant != nil && !sqliteNull(constant) {
			fill = constant
		}
		for i, element := range series {
			if seriesMissing(element, argument(2)) {
				result[i] = fill
			} else {
				result[i] = seriesJSON(element)
			}
		}
	case "forward", "backward":
		placeholder := argument(1)
		var last any
		indexes := make([]int, len(series))
		for i := range indexes {
			indexes[i] = i
			if kind == "backward" {
				indexes[i] = len(series) - 1 - i
			}
		}
		for _, i := range indexes {
			if seriesMissing(series[i], placeholder) {
				result[i] = last
			} else {
				last = seriesJSON(series[i])
				result[i] = last
			}
		}
	case "linear":
		placeholder, fillEdges, constant := argument(1), true, argument(3)
		if flag, ok := argument(2).(int64); ok {
			fillEdges = flag != 0
		}
		known, integral := []int{}, true
		for i, element := range series {
			if seriesMissing(element, placeholder) {
				continue
			}
			known = append(known, i)
			integral = integral && element.isInt
			result[i] = seriesJSON(element)
		}
		if len(known) == 0 {
			// An all-missing series is filled with constant_value (0 by default);
			// an explicit null constant preserves the nulls.
			var fill any = int64(0)
			if len(values) > 3 {
				fill = constant
				if sqliteNull(constant) {
					fill = nil
				}
			}
			for i := range result {
				result[i] = fill
			}
			break
		}
		for k := 0; k+1 < len(known); k++ {
			left, right := known[k], known[k+1]
			for i := left + 1; i < right; i++ {
				ratio := float64(i-left) / float64(right-left)
				interpolated := series[left].number + ratio*(series[right].number-series[left].number)
				if integral {
					// Integer series keep their element type, as Kusto does.
					result[i] = int64(interpolated)
				} else {
					result[i] = finiteJSON(interpolated)
				}
			}
		}
		for i := 0; i < known[0]; i++ {
			result[i] = edgeFill(fillEdges, seriesJSON(series[known[0]]))
		}
		for i := known[len(known)-1] + 1; i < len(series); i++ {
			result[i] = edgeFill(fillEdges, seriesJSON(series[known[len(known)-1]]))
		}
	default:
		return nil, fmt.Errorf("unknown series fill %s", kind)
	}
	return encodeSeries(result)
}

func edgeFill(fillEdges bool, nearest any) any {
	if fillEdges {
		return nearest
	}
	return nil
}

func validSeries(series []seriesValue) []float64 {
	values := make([]float64, 0, len(series))
	for _, element := range series {
		if element.valid {
			values = append(values, element.number)
		}
	}
	return values
}

// kqlSeriesStats returns min, min_idx, max, max_idx, avg, stdev and variance
// (sample) over the numeric elements, as series_stats_dynamic.
func kqlSeriesStats(value any) (any, error) {
	series, ok := parseSeries(value)
	if !ok {
		return nil, nil
	}
	stats := map[string]any{"min": nil, "min_idx": nil, "max": nil, "max_idx": nil, "avg": nil, "stdev": nil, "variance": nil, "sum": nil, "len": int64(len(series))}
	count, sum := 0, 0.0
	for i, element := range series {
		if !element.valid {
			continue
		}
		if stats["min"] == nil || element.number < stats["min"].(float64) {
			stats["min"], stats["min_idx"] = element.number, int64(i)
		}
		if stats["max"] == nil || element.number > stats["max"].(float64) {
			stats["max"], stats["max_idx"] = element.number, int64(i)
		}
		count++
		sum += element.number
	}
	if count > 0 {
		mean := sum / float64(count)
		stats["avg"], stats["sum"] = mean, sum
		if count > 1 {
			squares := 0.0
			for _, x := range validSeries(series) {
				squares += (x - mean) * (x - mean)
			}
			variance := squares / float64(count-1)
			stats["variance"], stats["stdev"] = variance, math.Sqrt(variance)
		} else {
			stats["variance"], stats["stdev"] = 0.0, 0.0
		}
	}
	encoded, err := json.Marshal(stats)
	return string(encoded), err
}

// kqlSeriesFitLine fits y = slope*x + interception over x = 1..n (nulls as
// missing), returning rsquare, slope, variance, rvariance, interception and
// line_fit as series_fit_line_dynamic does.
func kqlSeriesFitLine(value any) (any, error) {
	series, ok := parseSeries(value)
	if !ok {
		return nil, nil
	}
	n, sx, sy, sxx, sxy := 0.0, 0.0, 0.0, 0.0, 0.0
	for i, element := range series {
		if !element.valid {
			continue
		}
		x := float64(i + 1)
		n++
		sx += x
		sy += element.number
		sxx += x * x
		sxy += x * element.number
	}
	result := map[string]any{"rsquare": nil, "slope": nil, "variance": nil, "rvariance": nil, "interception": nil, "line_fit": []any{}}
	if n >= 2 && n*sxx-sx*sx != 0 {
		slope := (n*sxy - sx*sy) / (n*sxx - sx*sx)
		intercept := (sy - slope*sx) / n
		mean := sy / n
		total, residual := 0.0, 0.0
		fit := make([]any, len(series))
		for i, element := range series {
			predicted := slope*float64(i+1) + intercept
			fit[i] = predicted
			if element.valid {
				total += (element.number - mean) * (element.number - mean)
				residual += (element.number - predicted) * (element.number - predicted)
			}
		}
		rsquare := 1.0
		if total != 0 {
			rsquare = 1 - residual/total
		}
		result["rsquare"], result["slope"], result["interception"], result["line_fit"] = rsquare, slope, intercept, fit
		result["variance"], result["rvariance"] = total/n, residual/n
	}
	encoded, err := json.Marshal(result)
	return string(encoded), err
}

// kqlSeriesOutliers scores elements with Tukey's test over the percentile
// range: 0 inside the fences, otherwise the distance in interquartile ranges
// beyond them (positive above, negative below).
func kqlSeriesOutliers(value, kindValue, ignoreValue, minimumValue, maximumValue any) (any, error) {
	series, ok := parseSeries(value)
	if !ok {
		return nil, nil
	}
	kind := "ctukey"
	if text, ok := textArgument(kindValue); ok && text != "" && !sqliteNull(kindValue) {
		kind = strings.ToLower(text)
	}
	if kind != "ctukey" && kind != "tukey" {
		return nil, fmt.Errorf("series_outliers kind must be tukey or ctukey")
	}
	low, high := 10.0, 90.0
	if kind == "tukey" {
		low, high = 25, 75
	}
	if number, ok := realArgument(minimumValue); ok {
		low = number
	}
	if number, ok := realArgument(maximumValue); ok {
		high = number
	}
	ignored, hasIgnored := realArgument(ignoreValue)
	values := []float64{}
	for _, element := range series {
		if element.valid && !(hasIgnored && element.number == ignored) {
			values = append(values, element.number)
		}
	}
	result := make([]any, len(series))
	if len(values) == 0 {
		return encodeSeries(result)
	}
	sort.Float64s(values)
	percentile := func(p float64) float64 {
		position := p / 100 * float64(len(values)-1)
		lower := math.Floor(position)
		if int(lower)+1 >= len(values) {
			return values[len(values)-1]
		}
		return values[int(lower)] + (position-lower)*(values[int(lower)+1]-values[int(lower)])
	}
	q1, q3 := percentile(low), percentile(high)
	iqr := q3 - q1
	for i, element := range series {
		switch {
		case !element.valid || (hasIgnored && element.number == ignored):
			result[i] = 0.0
		case iqr == 0:
			result[i] = 0.0
		case element.number > q3:
			result[i] = (element.number - q3) / iqr
		case element.number < q1:
			result[i] = -(q1 - element.number) / iqr
		default:
			result[i] = 0.0
		}
	}
	return encodeSeries(result)
}

// kqlSeriesFIR applies a finite impulse response filter. With normalize the
// coefficients are divided by their sum; with center the window is centered.
func kqlSeriesFIR(value, filterValue, normalizeValue, centerValue any) (any, error) {
	series, ok := parseSeries(value)
	filter, okFilter := parseSeries(filterValue)
	if !ok || !okFilter || len(filter) == 0 {
		return nil, nil
	}
	coefficients := validSeries(filter)
	if len(coefficients) != len(filter) {
		return nil, nil
	}
	// normalize defaults to true unless the filter has a negative coefficient;
	// normalizing such a filter yields null, as Kusto documents.
	negative, sum := false, 0.0
	for _, c := range coefficients {
		negative = negative || c < 0
		sum += c
	}
	normalize, center := !negative, false
	if flag, ok := normalizeValue.(int64); ok {
		normalize = flag != 0
	}
	if flag, ok := centerValue.(int64); ok {
		center = flag != 0
	}
	if normalize && (negative || sum == 0) {
		return nil, nil
	}
	shift := 0
	if center {
		shift = len(coefficients) / 2
	}
	// Points outside the series count as zero, so the filter output keeps the
	// series length; a null inside the window makes that point null.
	result := make([]any, len(series))
	for i := range series {
		total, complete := 0.0, true
		for k, c := range coefficients {
			j := i - k + shift
			if j < 0 || j >= len(series) {
				continue
			}
			if !series[j].valid {
				complete = false
				break
			}
			total += c * series[j].number
		}
		if complete {
			if normalize {
				total /= sum
			}
			result[i] = finiteJSON(total)
		}
	}
	return encodeSeries(result)
}

// kqlSeriesIIR applies y[n] = (sum b[k]x[n-k] - sum_{k>0} a[k]y[n-k]) / a[0].
func kqlSeriesIIR(value, numeratorsValue, denominatorsValue any) (any, error) {
	series, ok := parseSeries(value)
	numerators, okN := parseSeries(numeratorsValue)
	denominators, okD := parseSeries(denominatorsValue)
	if !ok || !okN || !okD || len(denominators) == 0 || !denominators[0].valid || denominators[0].number == 0 {
		return nil, nil
	}
	b, a := validSeries(numerators), validSeries(denominators)
	output := make([]float64, len(series))
	result := make([]any, len(series))
	for i := range series {
		value := 0.0
		for k, coefficient := range b {
			if i-k >= 0 && series[i-k].valid {
				value += coefficient * series[i-k].number
			}
		}
		for k := 1; k < len(a); k++ {
			if i-k >= 0 {
				value -= a[k] * output[i-k]
			}
		}
		output[i] = value / a[0]
		result[i] = finiteJSON(output[i])
	}
	return encodeSeries(result)
}

// kqlSeriesReduce computes magnitude, product, dot product, cosine similarity
// or Pearson correlation.
func kqlSeriesReduce(operation string, values ...any) any {
	first, ok := parseSeries(values[0])
	if !ok {
		return nil
	}
	switch operation {
	case "magnitude":
		sum := 0.0
		for _, x := range validSeries(first) {
			sum += x * x
		}
		return finite(math.Sqrt(sum))
	case "product":
		product, integral := 1.0, true
		var integer int64 = 1
		for _, element := range first {
			if !element.valid {
				continue
			}
			product *= element.number
			if element.isInt && math.Abs(float64(integer)*element.number) < 9e18 {
				integer *= element.integer
			} else {
				integral = false
			}
		}
		if integral {
			return integer
		}
		return finite(product)
	}
	if len(values) < 2 {
		return nil
	}
	second, ok := parseSeries(values[1])
	if !ok {
		return nil
	}
	n := len(first)
	if len(second) < n {
		n = len(second)
	}
	dot, aa, bb := 0.0, 0.0, 0.0
	xs, ys := []float64{}, []float64{}
	for i := 0; i < n; i++ {
		if !first[i].valid || !second[i].valid {
			continue
		}
		x, y := first[i].number, second[i].number
		dot += x * y
		aa += x * x
		bb += y * y
		xs, ys = append(xs, x), append(ys, y)
	}
	switch operation {
	case "dot_product":
		return finite(dot)
	case "cosine_similarity":
		if aa == 0 || bb == 0 {
			return nil
		}
		return finite(dot / math.Sqrt(aa*bb))
	case "pearson_correlation":
		if len(xs) < 2 {
			return nil
		}
		mx, my := 0.0, 0.0
		for i := range xs {
			mx, my = mx+xs[i], my+ys[i]
		}
		mx, my = mx/float64(len(xs)), my/float64(len(ys))
		cov, vx, vy := 0.0, 0.0, 0.0
		for i := range xs {
			cov += (xs[i] - mx) * (ys[i] - my)
			vx += (xs[i] - mx) * (xs[i] - mx)
			vy += (ys[i] - my) * (ys[i] - my)
		}
		if vx == 0 || vy == 0 {
			return nil
		}
		return finite(cov / math.Sqrt(vx*vy))
	}
	return nil
}
