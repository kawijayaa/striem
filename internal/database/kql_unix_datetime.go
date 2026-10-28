package database

import (
	"math"
	"math/big"
	"strconv"
	"time"

	"github.com/kawijayaa/striem/internal/eventtime"
)

func unixKQLDatetime(value any, unitsPerSecond int64) any {
	var number *big.Rat
	switch input := value.(type) {
	case int64:
		number = new(big.Rat).SetInt64(input)
	case float64:
		if math.IsNaN(input) || math.IsInf(input, 0) {
			return nil
		}
		// Decimal round-trip text avoids a second binary64 multiplication losing
		// ticks. Exact engine floating-point quantization remains a conformance case.
		var ok bool
		number, ok = new(big.Rat).SetString(strconv.FormatFloat(input, 'g', -1, 64))
		if !ok {
			return nil
		}
	default:
		return nil
	}
	number.Mul(number, big.NewRat(10000000, unitsPerSecond))
	ticks := new(big.Int).Quo(number.Num(), number.Denom())
	if !ticks.IsInt64() {
		return nil
	}
	valueTicks := ticks.Int64()
	if valueTicks < -621355968000000000 || valueTicks > 2534023007999999999 {
		return nil
	}
	seconds, fraction := valueTicks/10000000, valueTicks%10000000
	if fraction < 0 {
		seconds--
		fraction += 10000000
	}
	return eventtime.Format(time.Unix(seconds, fraction*100))
}

func kqlUnixSeconds(value any) any      { return unixKQLDatetime(value, 1) }
func kqlUnixMilliseconds(value any) any { return unixKQLDatetime(value, 1000) }
func kqlUnixMicroseconds(value any) any { return unixKQLDatetime(value, 1000000) }
func kqlUnixNanoseconds(value any) any  { return unixKQLDatetime(value, 1000000000) }
