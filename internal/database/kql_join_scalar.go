package database

import (
	"fmt"
	"time"
)

func canonicalKQLDatetime(value any) (string, error) {
	raw, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("datetime scalar representation is unsupported")
	}
	date, err := time.Parse(time.RFC3339Nano, raw)
	date = date.UTC()
	if err != nil || date.Year() < 1 || date.Year() > 9999 || date.Nanosecond()%100 != 0 {
		return "", fmt.Errorf("scalar conversion requires a valid datetime with 100ns precision")
	}
	return date.Format("2006-01-02T15:04:05.0000000Z"), nil
}

func kqlJoinScalarText(value any, kind string) (any, error) {
	if value == nil {
		return nil, nil
	}
	if blob, ok := value.([]byte); ok && blob == nil {
		return nil, nil
	}
	if kind == "datetime" {
		text, err := canonicalKQLDatetime(value)
		if err != nil {
			return nil, err
		}
		return text, nil
	}
	return kqlScalarText(value, kind), nil
}
