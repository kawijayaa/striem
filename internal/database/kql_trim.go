package database

import "fmt"

func kqlTrim(pattern, value any) (any, error)      { return trimRegex(pattern, value, "both") }
func kqlTrimStart(pattern, value any) (any, error) { return trimRegex(pattern, value, "start") }
func kqlTrimEnd(pattern, value any) (any, error)   { return trimRegex(pattern, value, "end") }

func trimRegex(patternValue, valueValue any, mode string) (any, error) {
	pattern, ok := patternValue.(string)
	if !ok {
		return nil, nil
	}
	value, ok := valueValue.(string)
	if !ok {
		return nil, nil
	}
	if len(pattern) > 512 {
		return nil, fmt.Errorf("trim pattern exceeds 512 bytes")
	}
	if len(value) > maxReplacementBytes {
		return nil, fmt.Errorf("trim input exceeds %d bytes", maxReplacementBytes)
	}
	// Absolute anchors cannot be changed into line anchors by (?m).
	anchored := `\A(?:` + pattern + `)+|(?:` + pattern + `)+\z`
	if mode == "start" {
		anchored = `\A(?:` + pattern + `)`
	}
	if mode == "end" {
		anchored = `(?:` + pattern + `)\z`
	}
	expression, err := compileRegexp(anchored)
	if err != nil {
		return nil, err
	}
	if mode == "both" {
		return expression.ReplaceAllString(value, ""), nil
	}
	match := expression.FindStringIndex(value)
	if match == nil {
		return value, nil
	}
	if mode == "start" {
		return value[match[1]:], nil
	}
	return value[:match[0]], nil
}
