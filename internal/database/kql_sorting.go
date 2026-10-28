package database

import (
	"encoding/json"
	"math"
	"math/big"
	"math/rand/v2"
	"sort"
	"strings"
)

// sortRank orders mixed array elements as documented for array_sort_asc:
// numbers (and datetimes/timespans) first, then strings, then everything else.
func sortRank(value any) int {
	switch value.(type) {
	case float64, json.Number:
		return 0
	case string:
		return 1
	}
	return 2
}

func compareJSON(left, right any) int {
	if rank := sortRank(left) - sortRank(right); rank != 0 {
		return rank
	}
	switch a := left.(type) {
	case json.Number:
		x, _ := new(big.Float).SetString(a.String())
		y, _ := new(big.Float).SetString(right.(json.Number).String())
		if x == nil || y == nil {
			return strings.Compare(a.String(), right.(json.Number).String())
		}
		return x.Cmp(y)
	case string:
		return strings.Compare(a, right.(string))
	}
	encodedLeft, _ := json.Marshal(left)
	encodedRight, _ := json.Marshal(right)
	return strings.Compare(string(encodedLeft), string(encodedRight))
}

// kqlArraySort implements array_sort_asc/desc: the first array is sorted
// (nulls last unless nullsLast is false) and the other arrays follow its
// permutation; arrays of a different length become null. One array returns
// the sorted array; several return an array of arrays.
func kqlArraySort(descending bool) func(...any) (any, error) {
	return func(values ...any) (any, error) {
		nullsLast := true
		if len(values) > 1 {
			if flag, ok := values[len(values)-1].(int64); ok {
				nullsLast, values = flag != 0, values[:len(values)-1]
			}
		}
		arrays := make([][]any, len(values))
		for i, value := range values {
			text, ok := textArgument(value)
			if !ok || sqliteNull(value) {
				if i == 0 {
					return nil, nil
				}
				continue
			}
			decoder := json.NewDecoder(strings.NewReader(text))
			decoder.UseNumber()
			if err := decoder.Decode(&arrays[i]); err != nil {
				if i == 0 {
					return nil, nil
				}
				arrays[i] = nil
			}
		}
		first := arrays[0]
		if len(first) > maxCollectionValues {
			return nil, nil
		}
		order := make([]int, len(first))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(i, j int) bool {
			a, b := first[order[i]], first[order[j]]
			if a == nil || b == nil {
				if a == nil && b == nil {
					return false
				}
				return (b == nil) == nullsLast
			}
			if descending {
				return compareJSON(a, b) > 0
			}
			return compareJSON(a, b) < 0
		})
		results := make([]any, len(arrays))
		for i, array := range arrays {
			if array == nil || len(array) != len(first) {
				results[i] = nil
				continue
			}
			sorted := make([]any, len(array))
			for position, index := range order {
				sorted[position] = array[index]
			}
			results[i] = sorted
		}
		var output any = results
		if len(results) == 1 {
			output = results[0]
		}
		encoded, err := json.Marshal(output)
		return string(encoded), err
	}
}

// kqlBinAt rounds value down to a multiple of size measured from fixed:
// fixed + floor((value - fixed) / size) * size, for numbers and ticks.
func kqlBinAt(value, size, fixed any) any {
	if integer, ok := value.(int64); ok {
		step, okStep := size.(int64)
		origin, okOrigin := fixed.(int64)
		if okStep && okOrigin {
			if step <= 0 {
				return nil
			}
			offset := new(big.Int).Sub(big.NewInt(integer), big.NewInt(origin))
			quotient := new(big.Int).Div(offset, big.NewInt(step))
			result := new(big.Int).Add(big.NewInt(origin), quotient.Mul(quotient, big.NewInt(step)))
			if !result.IsInt64() {
				return nil
			}
			return result.Int64()
		}
	}
	x, ok := realArgument(value)
	step, okStep := realArgument(size)
	origin, okOrigin := realArgument(fixed)
	if !ok || !okStep || !okOrigin || step <= 0 {
		return nil
	}
	return finite(origin + math.Floor((x-origin)/step)*step)
}

// kqlBinAtDatetime bins a datetime by a tick size aligned to a fixed datetime.
func kqlBinAtDatetime(value, sizeValue, fixedValue any) any {
	when, ok := parsedDatetime(value)
	origin, valid := parsedDatetime(fixedValue)
	size, okSize := sizeValue.(int64)
	if !ok || !valid || !okSize || size <= 0 {
		return nil
	}
	offset := new(big.Int).Sub(datetimeTicks(when), datetimeTicks(origin))
	quotient := new(big.Int).Div(offset, big.NewInt(size))
	return ticksDatetime(new(big.Int).Add(datetimeTicks(origin), quotient.Mul(quotient, big.NewInt(size))))
}

// kqlRand returns a real in [0, 1), or a long in [0, n) for a positive n.
func kqlRand(values ...any) any {
	if len(values) == 0 {
		return rand.Float64()
	}
	limit, ok := values[0].(int64)
	if !ok || limit <= 0 {
		return nil
	}
	return rand.Int64N(limit)
}
