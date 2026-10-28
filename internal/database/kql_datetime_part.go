package database

import "strings"

func kqlDatetimePart(partValue, dateValue any) any {
	part, ok := partValue.(string)
	date, valid := parsedDatetime(dateValue)
	if !ok || !valid || len(part) > 16 {
		return nil
	}
	switch strings.ToLower(part) {
	case "year":
		return int64(date.Year())
	case "quarter":
		return int64((date.Month()-1)/3 + 1)
	case "month":
		return int64(date.Month())
	case "week_of_year":
		_, week := date.ISOWeek()
		return int64(week)
	case "day":
		return int64(date.Day())
	case "dayofyear":
		return int64(date.YearDay())
	case "hour":
		return int64(date.Hour())
	case "minute":
		return int64(date.Minute())
	case "second":
		return int64(date.Second())
	case "millisecond":
		return int64(date.Nanosecond() / 1000000)
	case "microsecond":
		return int64(date.Nanosecond() / 1000)
	case "nanosecond":
		return int64(date.Nanosecond() / 100 * 100)
	}
	return nil
}
