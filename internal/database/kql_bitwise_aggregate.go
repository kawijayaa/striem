package database

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type bitwiseAggregate struct {
	operation string
	value     int64
	seen      bool
}

func (a *bitwiseAggregate) Step(value any) error {
	if value == nil {
		return nil
	}
	if b, ok := value.([]byte); ok && b == nil {
		return nil
	}
	number, ok := value.(int64)
	if !ok {
		return fmt.Errorf("bitwise aggregate requires integer values")
	}
	if !a.seen {
		a.value = number
		a.seen = true
		return nil
	}
	switch a.operation {
	case "and":
		a.value &= number
	case "or":
		a.value |= number
	case "xor":
		a.value ^= number
	default:
		return fmt.Errorf("unsupported bitwise aggregate")
	}
	return nil
}
func (a *bitwiseAggregate) Done() any {
	if !a.seen {
		return nil
	}
	return a.value
}

func kqlToHex(value any, kind string, widthValue any) any {
	number, ok := value.(int64)
	width, valid := widthValue.(int64)
	if !ok || !valid || width < 1 {
		return nil
	}
	if width > 16 {
		width = 16
	}
	bits := uint64(number)
	if kind == "int" {
		if number < math.MinInt32 || number > math.MaxInt32 {
			return nil
		}
		bits = uint64(uint32(number))
	}
	text := strconv.FormatUint(bits, 16)
	if int64(len(text)) < width {
		text = strings.Repeat("0", int(width)-len(text)) + text
	}
	return text
}
