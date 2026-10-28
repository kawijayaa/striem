package database

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const maxFormatBytes = 256

// formatRuns splits a Kusto format string into runs of one specifier letter;
// other characters (the documented delimiters and any literal text) pass
// through unchanged.
func formatRuns(format string, specifiers string, write func(letter byte, count int) (string, bool)) (string, bool) {
	if len(format) > maxFormatBytes {
		return "", false
	}
	var out strings.Builder
	for i := 0; i < len(format); {
		letter := format[i]
		if !strings.ContainsRune(specifiers, rune(letter)) {
			out.WriteByte(letter)
			i++
			continue
		}
		count := 1
		for i+count < len(format) && format[i+count] == letter {
			count++
		}
		text, ok := write(letter, count)
		if !ok {
			return "", false
		}
		// An F run with nothing to show also drops a dot written just before it.
		if text == "" && letter == 'F' && strings.HasSuffix(out.String(), ".") {
			trimmed := strings.TrimSuffix(out.String(), ".")
			out.Reset()
			out.WriteString(trimmed)
		}
		out.WriteString(text)
		i += count
	}
	return out.String(), true
}

// fraction renders the first count digits of a 7-digit tick fraction; the F
// forms drop trailing zeros and print nothing for zero.
func fraction(ticks int64, letter byte, count int) (string, bool) {
	if count > 7 {
		return "", false
	}
	digits := fmt.Sprintf("%07d", ticks)[:count]
	if letter == 'F' {
		digits = strings.TrimRight(digits, "0")
	}
	return digits, true
}

func padded(value int64, count int) string {
	if count < 2 {
		return strconv.FormatInt(value, 10)
	}
	return fmt.Sprintf("%0*d", count, value)
}

func kqlFormatDatetime(value, formatValue any) any {
	when, ok := parsedDatetime(value)
	format, valid := textArgument(formatValue)
	if !ok || !valid {
		return nil
	}
	ticks := int64(when.Nanosecond() / 100)
	text, ok := formatRuns(format, "dfFhHmMsyt", func(letter byte, count int) (string, bool) {
		switch letter {
		case 'd':
			if count > 2 {
				return "", false
			}
			return padded(int64(when.Day()), count), true
		case 'f', 'F':
			return fraction(ticks, letter, count)
		case 'h':
			hour := when.Hour() % 12
			if hour == 0 {
				hour = 12
			}
			return padded(int64(hour), min(count, 2)), true
		case 'H':
			return padded(int64(when.Hour()), min(count, 2)), true
		case 'm':
			return padded(int64(when.Minute()), min(count, 2)), true
		case 'M':
			if count > 2 {
				return "", false
			}
			return padded(int64(when.Month()), count), true
		case 's':
			return padded(int64(when.Second()), min(count, 2)), true
		case 'y':
			switch {
			case count == 1:
				return strconv.Itoa(when.Year() % 100), true
			case count == 2:
				return fmt.Sprintf("%02d", when.Year()%100), true
			}
			return padded(int64(when.Year()), max(count, 4)), true
		case 't':
			if when.Hour() < 12 {
				return "AM"[:min(count, 2)], true
			}
			return "PM"[:min(count, 2)], true
		}
		return "", false
	})
	if !ok {
		return nil
	}
	return text
}

func kqlFormatTimespan(value, formatValue any) any {
	ticks, ok := value.(int64)
	format, valid := textArgument(formatValue)
	if !ok || !valid {
		return nil
	}
	sign := ""
	magnitude := uint64(ticks)
	if ticks < 0 {
		sign, magnitude = "-", uint64(-ticks)
	}
	days := int64(magnitude / 864000000000)
	text, ok := formatRuns(format, "dfFhHms", func(letter byte, count int) (string, bool) {
		switch letter {
		case 'd':
			if count > 8 {
				return "", false
			}
			return padded(days, count), true
		case 'f', 'F':
			return fraction(int64(magnitude%10000000), letter, count)
		case 'h', 'H':
			return padded(int64(magnitude/36000000000%24), min(count, 2)), true
		case 'm':
			return padded(int64(magnitude/600000000%60), min(count, 2)), true
		case 's':
			return padded(int64(magnitude/10000000%60), min(count, 2)), true
		}
		return "", false
	})
	if !ok {
		return nil
	}
	return sign + text
}

// kqlMakeTimespan builds ticks from (hour, minute[, second]) or
// (day, hour, minute, second), returning null for out-of-range parts.
func kqlMakeTimespan(values ...any) any {
	parts := make([]float64, len(values))
	for i, value := range values {
		number, ok := realArgument(value)
		if !ok {
			return nil
		}
		parts[i] = number
	}
	var day, hour, minute, second float64
	switch len(parts) {
	case 2:
		hour, minute = parts[0], parts[1]
	case 3:
		hour, minute, second = parts[0], parts[1], parts[2]
	case 4:
		day, hour, minute, second = parts[0], parts[1], parts[2], parts[3]
	default:
		return nil
	}
	if day != math.Trunc(day) || hour != math.Trunc(hour) || minute != math.Trunc(minute) ||
		day < 0 || day > 10675198 || hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second >= 60 {
		return nil
	}
	ticks := int64(day)*864000000000 + int64(hour)*36000000000 + int64(minute)*600000000 + int64(math.Round(second*10000000))
	return ticks
}
