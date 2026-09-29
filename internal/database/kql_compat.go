package database

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/kawijayaa/striem/internal/eventtime"
	"github.com/mattn/go-sqlite3"
)

func registerKQLCompatibility(c *sqlite3.SQLiteConn) error {
	for name, function := range map[string]any{
		"kql_bin": kqlBin, "kql_bin_datetime": kqlBinDatetime,
		"kql_calendar": kqlCalendar,
	} {
		if err := c.RegisterFunc(name, function, true); err != nil {
			return err
		}
	}
	return c.RegisterAggregator("kql_count_distinct", func() *distinctCount { return &distinctCount{seen: make(map[string]struct{})} }, true)
}

func number(value any) (float64, bool) {
	switch v := value.(type) {
	case int64:
		return float64(v), true
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	}
	return 0, false
}

func kqlBin(value, size any) any {
	// Keep long bins exact beyond the precision available in a float64.
	if v, ok := value.(int64); ok {
		if s, ok := size.(int64); ok {
			if s <= 0 {
				return nil
			}
			rem := v % s
			if rem < 0 {
				rem += s
			}
			if v < math.MinInt64+rem {
				return nil
			}
			return v - rem
		}
	}
	v, vok := number(value)
	s, sok := number(size)
	if !vok || !sok || s <= 0 {
		return nil
	}
	result := math.Floor(v/s) * s
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return nil
	}
	return result
}

func parsedDatetime(value any) (time.Time, bool) {
	normalized, ok := kqlToDatetime(value).(string)
	if !ok {
		return time.Time{}, false
	}
	parsed, err := time.Parse(eventtime.Layout, normalized)
	return parsed, err == nil && parsed.Year() >= 1 && parsed.Year() <= 9999
}

func kqlBinDatetime(value any, size int64) any {
	valueTime, ok := parsedDatetime(value)
	if !ok || size <= 0 {
		return nil
	}
	// UnixNano overflows outside 1678–2262. Big integers keep all KQL years
	// and subsecond boundaries exact, including dates before the Unix epoch.
	nanos := new(big.Int).Mul(big.NewInt(valueTime.Unix()), big.NewInt(1e9))
	nanos.Add(nanos, big.NewInt(int64(valueTime.Nanosecond())))
	remainder := new(big.Int).Mod(nanos, big.NewInt(size))
	nanos.Sub(nanos, remainder)
	seconds, fraction := new(big.Int), new(big.Int)
	seconds.DivMod(nanos, big.NewInt(1e9), fraction)
	result := time.Unix(seconds.Int64(), fraction.Int64()).UTC()
	if result.Year() < 1 || result.Year() > 9999 {
		return nil
	}
	return eventtime.Format(result)
}

func kqlCalendar(value any, unit string, offsetValue any, end bool) any {
	v, ok := parsedDatetime(value)
	offset, valid := offsetValue.(int64)
	// Bound before converting or multiplying offsets, including on 32-bit hosts.
	if !ok || !valid || offset < -4000000 || offset > 4000000 {
		return nil
	}
	year, month, day := v.Date()
	var start time.Time
	shift := int(offset)
	if end {
		shift++
	}
	switch unit {
	case "day":
		start = time.Date(year, month, day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, shift)
	case "week":
		start = time.Date(year, month, day-int(v.Weekday()), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 7*shift)
	case "month":
		start = time.Date(year, month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, shift, 0)
	case "year":
		start = time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(shift, 0, 0)
	default:
		return nil
	}
	// Kusto datetimes have 100ns precision.
	if end {
		start = start.Add(-100 * time.Nanosecond)
	}
	if start.Year() < 1 || start.Year() > 9999 {
		return nil
	}
	return eventtime.Format(start)
}

const maxDistinctValues = 100000
const maxDistinctBytes = 4 << 20

type distinctCount struct {
	seen  map[string]struct{}
	bytes int
}

func (a *distinctCount) Step(value any) error {
	if value == nil {
		return nil
	}
	if v, ok := value.([]byte); ok {
		if v == nil {
			return nil
		}
		value = string(v)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	key := string(encoded)
	if _, ok := a.seen[key]; ok {
		return nil
	}
	if len(a.seen) >= maxDistinctValues || a.bytes+len(key) > maxDistinctBytes {
		return fmt.Errorf("distinct count exceeds %d values or %d bytes per group", maxDistinctValues, maxDistinctBytes)
	}
	a.seen[key] = struct{}{}
	a.bytes += len(key)
	return nil
}
func (a *distinctCount) Done() int64 { return int64(len(a.seen)) }
