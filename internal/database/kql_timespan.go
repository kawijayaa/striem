package database

import (
	"math/big"
	"time"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/striem/internal/eventtime"
)

// Timespans are int64 ticks (100ns) in SQL, as compiled by KSQL. Datetimes are
// canonical UTC text; big integers keep years 1–9999 exact at tick precision.

func datetimeTicks(value time.Time) *big.Int {
	ticks := new(big.Int).Mul(big.NewInt(value.Unix()), big.NewInt(10000000))
	return ticks.Add(ticks, big.NewInt(int64(value.Nanosecond()/100)))
}

func ticksDatetime(ticks *big.Int) any {
	seconds, fraction := new(big.Int), new(big.Int)
	seconds.DivMod(ticks, big.NewInt(10000000), fraction)
	if !seconds.IsInt64() {
		return nil
	}
	result := time.Unix(seconds.Int64(), fraction.Int64()*100).UTC()
	if result.Year() < 1 || result.Year() > 9999 {
		return nil
	}
	return eventtime.Format(result)
}

func kqlDatetimeAddTicks(value, ticksValue any) any {
	when, ok := parsedDatetime(value)
	ticks, valid := ticksValue.(int64)
	if !ok || !valid {
		return nil
	}
	return ticksDatetime(new(big.Int).Add(datetimeTicks(when), big.NewInt(ticks)))
}

func kqlDatetimeDiffTicks(leftValue, rightValue any) any {
	left, ok := parsedDatetime(leftValue)
	right, valid := parsedDatetime(rightValue)
	if !ok || !valid {
		return nil
	}
	return new(big.Int).Sub(datetimeTicks(left), datetimeTicks(right)).Int64()
}

// kqlToTimespan converts timespan text, or passes through integral ticks.
func kqlToTimespan(value any) any {
	switch current := value.(type) {
	case int64:
		return current
	case string:
		if ticks, ok := ksql.ParseTimespan(current); ok {
			return ticks
		}
	}
	return nil
}

// kqlDayOfWeek returns the timespan since the preceding Sunday midnight.
func kqlDayOfWeek(value any) any {
	when, ok := parsedDatetime(value)
	if !ok {
		return nil
	}
	return int64(when.Weekday()) * 864000000000
}
