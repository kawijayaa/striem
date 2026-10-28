package database

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Microsoft's documented non-ISO datetime spellings: RFC 822, RFC 850 and
// Sortable, each with optional seconds and an optional GMT zone. Two-digit
// years follow Go's 1969-2068 window. The M/d/yyyy form is not in the format
// table; it reproduces the parse operator examples, which convert it.
var (
	rfc822DatetimePattern   = regexp.MustCompile(`^(?:([A-Za-z]{3}), )?(\d{1,2}) ([A-Za-z]{3}) (\d{2}) (\d{1,2}):(\d{2})(?::(\d{2}))?(?: GMT)?$`)
	rfc850DatetimePattern   = regexp.MustCompile(`^(?:([A-Za-z]{6,9}), )?(\d{1,2})-([A-Za-z]{3})-(\d{2}) (\d{1,2}):(\d{2})(?::(\d{2}))?(?: GMT)?$`)
	sortableDatetimePattern = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})[T ](\d{1,2}):(\d{2})(?::(\d{2}))?(?: GMT)?$`)
	usDatetimePattern       = regexp.MustCompile(`^(\d{1,2})([/-])(\d{1,2})[/-](\d{4})(?: (\d{1,2}):(\d{2})(?::(\d{2})(?:\.(\d{1,7}))?)?)?$`)
	// Unpadded ISO dates (1970-5-12) and the comma form datetime(2025, 6, 14[, h, m, s])
	// used in Microsoft's examples.
	shortDatePattern = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})$`)
	commaDatePattern = regexp.MustCompile(`^(\d{1,4})\s*,\s*(\d{1,2})\s*,\s*(\d{1,2})(?:\s*,\s*(\d{1,2})(?:\s*,\s*(\d{1,2})(?:\s*,\s*(\d{1,2}))?)?)?$`)
	datetimeMonths   = map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
)

func parseKQLDatetimeFormats(text string) (time.Time, bool) {
	text = strings.TrimSpace(text)
	if len(text) > 48 {
		return time.Time{}, false
	}
	for _, rfc := range []struct {
		pattern *regexp.Regexp
		full    bool
	}{{rfc822DatetimePattern, false}, {rfc850DatetimePattern, true}} {
		match := rfc.pattern.FindStringSubmatch(text)
		if match == nil {
			continue
		}
		month, ok := datetimeMonths[strings.ToLower(match[3])]
		if !ok {
			return time.Time{}, false
		}
		year := atoi(match[4])
		if year < 69 {
			year += 2000
		} else {
			year += 1900
		}
		value, ok := datetimeFromParts(year, int(month), atoi(match[2]), atoi(match[5]), atoi(match[6]), atoi(match[7]), 0)
		if !ok || (match[1] != "" && !weekdayMatches(match[1], value.Weekday(), rfc.full)) {
			return time.Time{}, false
		}
		return value, true
	}
	if match := sortableDatetimePattern.FindStringSubmatch(text); match != nil {
		return datetimeFromParts(atoi(match[1]), atoi(match[2]), atoi(match[3]), atoi(match[4]), atoi(match[5]), atoi(match[6]), 0)
	}
	if match := usDatetimePattern.FindStringSubmatch(text); match != nil {
		fraction := 0
		if match[8] != "" {
			fraction = atoi((match[8] + "000000")[:7])
		}
		return datetimeFromParts(atoi(match[4]), atoi(match[1]), atoi(match[3]), atoi(match[5]), atoi(match[6]), atoi(match[7]), fraction)
	}
	if match := shortDatePattern.FindStringSubmatch(text); match != nil {
		return datetimeFromParts(atoi(match[1]), atoi(match[2]), atoi(match[3]), 0, 0, 0, 0)
	}
	if match := commaDatePattern.FindStringSubmatch(text); match != nil {
		return datetimeFromParts(atoi(match[1]), atoi(match[2]), atoi(match[3]), atoi(match[4]), atoi(match[5]), atoi(match[6]), 0)
	}
	return time.Time{}, false
}

// RFC 822 uses abbreviated weekdays and RFC 850 full names. A weekday that
// contradicts the date makes the value invalid instead of silently ignored.
func weekdayMatches(name string, weekday time.Weekday, full bool) bool {
	expected := weekday.String()
	if !full {
		expected = expected[:3]
	}
	return strings.EqualFold(name, expected)
}

func datetimeFromParts(year, month, day, hour, minute, second, ticks int) (time.Time, bool) {
	if year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59 {
		return time.Time{}, false
	}
	value := time.Date(year, time.Month(month), day, hour, minute, second, ticks*100, time.UTC)
	if value.Day() != day {
		return time.Time{}, false
	}
	return value, true
}

func atoi(text string) int {
	if text == "" {
		return 0
	}
	value, _ := strconv.Atoi(text)
	return value
}
