package database

import (
	"fmt"
	"math"
	"strings"
	"time"
)

func floorDatetimePeriod(value, divisor int64) int64 {
	quotient := value / divisor
	if value%divisor < 0 {
		quotient--
	}
	return quotient
}

func datetimePeriodIndex(value time.Time, period string) (int64, bool) {
	switch period {
	case "year":
		return int64(value.Year()), true
	case "quarter":
		return int64(value.Year())*4 + int64(value.Month()-1)/3, true
	case "month":
		return int64(value.Year())*12 + int64(value.Month()-1), true
	case "week":
		return floorDatetimePeriod(floorDatetimePeriod(value.Unix(), 86400)+4, 7), true
	case "day":
		return floorDatetimePeriod(value.Unix(), 86400), true
	case "hour":
		return floorDatetimePeriod(value.Unix(), 3600), true
	case "minute":
		return floorDatetimePeriod(value.Unix(), 60), true
	case "second":
		return value.Unix(), true
	case "millisecond":
		return value.Unix()*1000 + int64(value.Nanosecond()/1000000), true
	case "microsecond":
		return value.Unix()*1000000 + int64(value.Nanosecond()/1000), true
	}
	return 0, false
}

func kqlDatetimeDiff(periodValue, leftValue, rightValue any) (any, error) {
	period, ok := periodValue.(string)
	left, leftOK := parsedDatetime(leftValue)
	right, rightOK := parsedDatetime(rightValue)
	if !ok || !leftOK || !rightOK || len(period) > 16 {
		return nil, nil
	}
	period = strings.ToLower(period)
	if period == "nanosecond" {
		// These tick indices and their difference fit int64 over years 1–9999.
		a := left.Unix()*10000000 + int64(left.Nanosecond()/100)
		b := right.Unix()*10000000 + int64(right.Nanosecond()/100)
		difference := a - b
		if difference > math.MaxInt64/100 || difference < math.MinInt64/100 {
			return nil, fmt.Errorf("datetime_diff nanosecond result exceeds signed long range")
		}
		return difference * 100, nil
	}
	a, valid := datetimePeriodIndex(left, period)
	b, _ := datetimePeriodIndex(right, period)
	if !valid {
		return nil, nil
	}
	return a - b, nil
}
