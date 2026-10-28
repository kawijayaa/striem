package database

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/kawijayaa/ksql"
)

const maxScalarRangeElements = 1 << 20

func kqlScalarRange(startValue, stopValue, stepValue any) (any, error) {
	values := []any{startValue, stopValue, stepValue}
	real := false
	for _, value := range values {
		switch value.(type) {
		case int64:
		case float64:
			real = true
		default:
			return nil, nil
		}
	}
	result := make([]byte, 0, 128)
	result = append(result, '[')
	if !real {
		current, stop, step := startValue.(int64), stopValue.(int64), stepValue.(int64)
		if step == 0 {
			return nil, nil
		}
		for count := 0; count < maxScalarRangeElements; count++ {
			if (step > 0 && current > stop) || (step < 0 && current < stop) {
				break
			}
			if count > 0 {
				result = append(result, ',')
			}
			result = strconv.AppendInt(result, current, 10)
			if current == stop || (step > 0 && current > math.MaxInt64-step) || (step < 0 && current < math.MinInt64-step) {
				break
			}
			current += step
		}
	} else {
		numbers := [3]float64{}
		for i, value := range values {
			switch number := value.(type) {
			case int64:
				numbers[i] = float64(number)
			case float64:
				numbers[i] = number
			}
			if math.IsInf(numbers[i], 0) || math.IsNaN(numbers[i]) {
				return nil, fmt.Errorf("range requires finite numeric values")
			}
		}
		current, stop, step := numbers[0], numbers[1], numbers[2]
		if step == 0 {
			return nil, nil
		}
		for count := 0; count < maxScalarRangeElements; count++ {
			if (step > 0 && current > stop) || (step < 0 && current < stop) {
				break
			}
			if count > 0 {
				result = append(result, ',')
			}
			result = strconv.AppendFloat(result, current, 'g', -1, 64)
			if current == stop || count+1 == maxScalarRangeElements {
				break
			}
			next := current + step
			if (step > 0 && next > stop) || (step < 0 && next < stop) {
				break
			}
			if next == current {
				return nil, fmt.Errorf("range step cannot advance the real value")
			}
			current = next
		}
	}
	result = append(result, ']')
	return string(result), nil
}

// kqlTemporalRange is range(datetime, datetime, timespan) or
// range(timespan, timespan, timespan): datetimes and Kusto timespan text in
// a dynamic array. The step is in ticks and must be non-zero.
func kqlTemporalRange(kind string, startValue, stopValue, stepValue any) (any, error) {
	step, ok := stepValue.(int64)
	if !ok || step == 0 {
		return nil, nil
	}
	var start, stop int64
	switch kind {
	case "datetime":
		from, okFrom := parsedDatetime(startValue)
		to, okTo := parsedDatetime(stopValue)
		if !okFrom || !okTo {
			return nil, nil
		}
		start, stop = datetimeTicks(from).Int64(), datetimeTicks(to).Int64()
	case "timespan":
		from, okFrom := startValue.(int64)
		to, okTo := stopValue.(int64)
		if !okFrom || !okTo {
			return nil, nil
		}
		start, stop = from, to
	default:
		return nil, fmt.Errorf("unknown range kind %s", kind)
	}
	var elements []any
	for current, count := start, 0; count < maxScalarRangeElements; count++ {
		if (step > 0 && current > stop) || (step < 0 && current < stop) {
			break
		}
		if kind == "datetime" {
			elements = append(elements, ticksDatetime(big.NewInt(current)))
		} else {
			elements = append(elements, ksql.FormatTimespan(current))
		}
		if (step > 0 && current > math.MaxInt64-step) || (step < 0 && current < math.MinInt64-step) {
			break
		}
		current += step
	}
	if elements == nil {
		elements = []any{}
	}
	encoded, err := json.Marshal(elements)
	return string(encoded), err
}
