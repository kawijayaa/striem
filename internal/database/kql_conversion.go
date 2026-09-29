package database

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

var decimalText = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func kqlToInteger(value any, bits int) any {
	var number int64
	switch v := value.(type) {
	case int64:
		number = v
	case float64:
		v = math.Trunc(v)
		if math.IsNaN(v) || math.IsInf(v, 0) || v < -9223372036854775808.0 || v >= 9223372036854775808.0 {
			return nil
		}
		number = int64(v)
	case string:
		var ok bool
		number, ok = decimalInteger(strings.TrimSpace(v))
		if !ok {
			return nil
		}
	default:
		return nil
	}
	if bits == 32 && (number < math.MinInt32 || number > math.MaxInt32) {
		return nil
	}
	return number
}

// Truncate decimal text without passing through float64. In particular, long
// values above 2^53 retain every bit. Exponents are bounded before allocating.
func decimalInteger(text string) (int64, bool) {
	if !decimalText.MatchString(text) {
		return 0, false
	}
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimLeft(text, "+-")
	exponent := 0
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		exponentText := text[index+1:]
		parsed, err := strconv.Atoi(exponentText)
		// Anything beyond the input length plus 20 either overflows a nonzero
		// long or truncates to zero, so no exponent-sized allocation is needed.
		limit := len(text) + 20
		if err != nil || parsed > limit || parsed < -limit {
			parsed = limit
			if strings.HasPrefix(exponentText, "-") {
				parsed = -limit
			}
		}
		exponent = parsed
		text = text[:index]
	}
	point := strings.IndexByte(text, '.')
	if point < 0 {
		point = len(text)
	} else {
		text = text[:point] + text[point+1:]
	}
	trimmed := strings.TrimLeft(text, "0")
	point -= len(text) - len(trimmed)
	if trimmed == "" {
		return 0, true
	}
	point += exponent
	if point <= 0 {
		return 0, true
	}
	if point > 19 {
		return 0, false
	}
	if len(trimmed) > point {
		trimmed = trimmed[:point]
	} else {
		trimmed += strings.Repeat("0", point-len(trimmed))
	}
	if negative {
		trimmed = "-" + trimmed
	}
	value, err := strconv.ParseInt(trimmed, 10, 64)
	return value, err == nil
}

func kqlToReal(value any) any {
	switch v := value.(type) {
	case int64:
		return float64(v)
	case float64:
		return v
	case string:
		text := strings.TrimSpace(v)
		if !decimalText.MatchString(text) {
			return nil
		}
		number, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil
		}
		return number
	default:
		return nil
	}
}

func kqlToBool(value any) any {
	switch v := value.(type) {
	case int64:
		return v != 0
	case float64:
		if math.IsNaN(v) {
			return nil
		}
		return v != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true":
			return true
		case "false":
			return false
		}
	}
	return nil
}
