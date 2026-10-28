package database

import (
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/kawijayaa/striem/internal/eventtime"
)

func kqlMakeDatetime(values ...any) any {
	if len(values) < 3 || len(values) > 6 {
		return nil
	}
	parts := [5]int64{}
	for i := 0; i < min(len(values), 5); i++ {
		number, ok := values[i].(int64)
		if !ok {
			return nil
		}
		parts[i] = number
	}
	year, month, day, hour, minute := parts[0], parts[1], parts[2], parts[3], parts[4]
	if year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return nil
	}
	seconds := float64(0)
	if len(values) == 6 {
		switch n := values[5].(type) {
		case int64:
			seconds = float64(n)
		case float64:
			seconds = n
		default:
			return nil
		}
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 59.9999999 {
		return nil
	}
	ticks := int64(math.Round(seconds * 1e7))
	if ticks >= 600000000 {
		return nil
	}
	result := time.Date(int(year), time.Month(month), int(day), int(hour), int(minute), int(ticks/1e7), int(ticks%1e7)*100, time.UTC)
	if result.Year() != int(year) || result.Month() != time.Month(month) || result.Day() != int(day) {
		return nil
	}
	return eventtime.Format(result)
}

func kqlDatetimeAdd(periodValue, amountValue, dateValue any) any {
	period, ok := periodValue.(string)
	amount, valid := amountValue.(int64)
	date, dateOK := parsedDatetime(dateValue)
	if !ok || !valid || !dateOK || len(period) > 16 {
		return nil
	}
	period = strings.ToLower(period)
	if period == "year" || period == "quarter" || period == "month" {
		if amount < -119988 || amount > 119988 {
			return nil
		}
		months := amount
		if period == "year" {
			months *= 12
		}
		if period == "quarter" {
			months *= 3
		}
		target := int64(date.Year()-1)*12 + int64(date.Month()-1) + months
		if target < 0 || target >= 9999*12 {
			return nil
		}
		year, month := int(target/12)+1, time.Month(target%12+1)
		lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
		result := time.Date(year, month, min(date.Day(), lastDay), date.Hour(), date.Minute(), date.Second(), date.Nanosecond()/100*100, time.UTC)
		return eventtime.Format(result)
	}
	units := map[string]int64{"week": 6048000000000, "day": 864000000000, "hour": 36000000000, "minute": 600000000, "second": 10000000, "millisecond": 10000, "microsecond": 10}
	unit, known := units[period]
	if period == "nanosecond" {
		// Kusto represents datetime in 100ns ticks. The exact sub-tick policy is
		// characterized separately; retain only whole ticks toward zero here.
		amount /= 100
		unit, known = 1, true
	}
	if !known {
		return nil
	}
	current := new(big.Int).Mul(big.NewInt(date.Unix()), big.NewInt(10000000))
	current.Add(current, big.NewInt(int64(date.Nanosecond()/100)))
	offset := new(big.Int).Mul(big.NewInt(amount), big.NewInt(unit))
	current.Add(current, offset)
	if !current.IsInt64() {
		return nil
	}
	ticks := current.Int64()
	seconds, fraction := ticks/10000000, ticks%10000000
	if fraction < 0 {
		seconds--
		fraction += 10000000
	}
	result := time.Unix(seconds, fraction*100).UTC()
	if result.Year() < 1 || result.Year() > 9999 {
		return nil
	}
	return eventtime.Format(result)
}
