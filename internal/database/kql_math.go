package database

import (
	"math"
	"math/big"
	"strconv"
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

// kqlRound rounds the shortest decimal representation of a number half away
// from zero, so round(2.15, 1) is 2.2 as documented rather than the binary
// neighbour 2.1. Negative precision rounds to tens, hundreds and so on.
func kqlRound(value, precisionValue any) any {
	precision := int64(0)
	if precisionValue != nil && !sqliteNull(precisionValue) {
		var ok bool
		if precision, ok = precisionValue.(int64); !ok {
			return nil
		}
	}
	// Doubles carry at most 17 significant digits and 308 exponent digits.
	if precision > 400 || precision < -400 {
		return nil
	}
	var text string
	switch number := value.(type) {
	case int64:
		text = strconv.FormatInt(number, 10)
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil
		}
		text = strconv.FormatFloat(number, 'f', -1, 64)
	default:
		return nil
	}
	exact, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil
	}
	scale := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(abs64(precision)), nil))
	if precision >= 0 {
		exact.Mul(exact, scale)
	} else {
		exact.Quo(exact, scale)
	}
	half := big.NewRat(1, 2)
	if exact.Sign() < 0 {
		exact.Sub(exact, half)
	} else {
		exact.Add(exact, half)
	}
	rounded := new(big.Int).Quo(exact.Num(), exact.Denom())
	result := new(big.Rat).SetInt(rounded)
	if precision >= 0 {
		result.Quo(result, scale)
	} else {
		result.Mul(result, scale)
	}
	if _, isInteger := value.(int64); isInteger {
		if !result.IsInt() || !result.Num().IsInt64() {
			return nil
		}
		return result.Num().Int64()
	}
	number, _ := result.Float64()
	return finite(number)
}

func abs64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

// sqlMathFunctions implements the SQL math functions KSQL emits. This SQLite
// build has no math extension, so without them sqrt/pow/exp/... failed with
// "no such function". Integer inputs to floor, ceiling and sign stay integral.
func sqlMathFunctions() map[string]any {
	unary := func(function func(float64) float64) func(any) any {
		return func(value any) any {
			x, ok := realArgument(value)
			if !ok {
				return nil
			}
			return finite(function(x))
		}
	}
	integral := func(function func(float64) float64) func(any) any {
		return func(value any) any {
			if integer, ok := value.(int64); ok {
				return integer
			}
			x, ok := realArgument(value)
			if !ok {
				return nil
			}
			return finite(function(x))
		}
	}
	domain := func(function func(float64) float64, valid func(float64) bool) func(any) any {
		return func(value any) any {
			x, ok := realArgument(value)
			if !ok || !valid(x) {
				return nil
			}
			return finite(function(x))
		}
	}
	power := func(base, exponent any) any {
		x, ok := realArgument(base)
		y, valid := realArgument(exponent)
		if !ok || !valid {
			return nil
		}
		return finite(math.Pow(x, y))
	}
	return map[string]any{
		"sqrt":    domain(math.Sqrt, func(x float64) bool { return x >= 0 }),
		"exp":     unary(math.Exp),
		"ln":      domain(math.Log, func(x float64) bool { return x > 0 }),
		"log10":   domain(math.Log10, func(x float64) bool { return x > 0 }),
		"pow":     power,
		"power":   power,
		"floor":   integral(math.Floor),
		"ceil":    integral(math.Ceil),
		"ceiling": integral(math.Ceil),
		"sin":     unary(math.Sin),
		"cos":     unary(math.Cos),
		"tan":     unary(math.Tan),
		"asin":    domain(math.Asin, func(x float64) bool { return x >= -1 && x <= 1 }),
		"acos":    domain(math.Acos, func(x float64) bool { return x >= -1 && x <= 1 }),
		"atan":    unary(math.Atan),
		"atan2": func(y, x any) any {
			a, ok := realArgument(y)
			b, valid := realArgument(x)
			if !ok || !valid {
				return nil
			}
			return finite(math.Atan2(a, b))
		},
		"sign": func(value any) any {
			if integer, ok := value.(int64); ok {
				switch {
				case integer > 0:
					return int64(1)
				case integer < 0:
					return int64(-1)
				}
				return int64(0)
			}
			x, ok := realArgument(value)
			if !ok {
				return nil
			}
			switch {
			case x > 0:
				return 1.0
			case x < 0:
				return -1.0
			}
			return 0.0
		},
	}
}
