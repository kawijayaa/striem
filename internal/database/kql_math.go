package database

import (
	"math"
	"strings"
	"unicode/utf8"
)

// finite keeps SQLite and JSON results representable: KQL's NaN and
// infinities have no SQLite storage form, so nonfinite results are null.
func finite(value float64) any {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return value
}

func realArgument(value any) (float64, bool) {
	switch number := value.(type) {
	case int64:
		return float64(number), true
	case float64:
		return number, true
	}
	return 0, false
}

var mathFunctions = map[string]func(float64) any{
	"degrees": func(x float64) any { return finite(x * 180 / math.Pi) },
	"radians": func(x float64) any { return finite(x * math.Pi / 180) },
	"cot":     func(x float64) any { return finite(1 / math.Tan(x)) },
	"exp2":    func(x float64) any { return finite(math.Exp2(x)) },
	"exp10":   func(x float64) any { return finite(math.Pow(10, x)) },
	"log2": func(x float64) any {
		if x < 0 {
			return nil
		}
		return finite(math.Log2(x))
	},
	"gamma": func(x float64) any { return finite(math.Gamma(x)) },
	"loggamma": func(x float64) any {
		value, _ := math.Lgamma(x)
		return finite(value)
	},
	"erf":  func(x float64) any { return finite(math.Erf(x)) },
	"erfc": func(x float64) any { return finite(math.Erfc(x)) },
}

func kqlMath(name string, value any) any {
	function, ok := mathFunctions[name]
	x, valid := realArgument(value)
	if !ok || !valid {
		return nil
	}
	return function(x)
}

// kqlNumberClass answers isfinite, isinf and isnan. SQLite stores NaN as null,
// so isnan is true only for values that reach the function as NaN.
func kqlNumberClass(name string, value any) any {
	x, ok := realArgument(value)
	if !ok {
		if name == "isfinite" {
			return nil
		}
		return false
	}
	switch name {
	case "isfinite":
		return !math.IsNaN(x) && !math.IsInf(x, 0)
	case "isinf":
		return math.IsInf(x, 0)
	default:
		return math.IsNaN(x)
	}
}

// kqlExtremeOf implements max_of/min_of over same-typed arguments: non-null
// values take precedence, and all-null arguments give null.
func kqlExtremeOf(maximum bool) func(...any) any {
	return func(values ...any) any {
		var best any
		for _, value := range values {
			if value == nil || sqliteNull(value) {
				continue
			}
			order := 0
			if best != nil {
				order = compareScalar(value, best)
			}
			if best == nil || (maximum && order > 0) || (!maximum && order < 0) {
				best = value
			}
		}
		return best
	}
}

func compareScalar(left, right any) int {
	if a, ok := left.(int64); ok {
		if b, ok := right.(int64); ok {
			switch {
			case a < b:
				return -1
			case a > b:
				return 1
			}
			return 0
		}
	}
	if a, ok := realArgument(left); ok {
		if b, ok := realArgument(right); ok {
			switch {
			case a < b:
				return -1
			case a > b:
				return 1
			}
			return 0
		}
	}
	a, _ := left.(string)
	b, _ := right.(string)
	return strings.Compare(a, b)
}

func kqlStrcmp(left, right any) any {
	a, ok := left.(string)
	b, valid := right.(string)
	if !ok || !valid {
		return nil
	}
	return int64(strings.Compare(a, b))
}

func kqlIsASCII(value any) any {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func kqlIsUTF8(value any) any {
	switch text := value.(type) {
	case string:
		return utf8.ValidString(text)
	case []byte:
		if text == nil {
			return nil
		}
		return utf8.Valid(text)
	}
	return nil
}
