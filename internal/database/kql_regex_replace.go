package database

import (
	"fmt"
	"strconv"
	"strings"
)

type regexRewritePart struct {
	text  string
	group int
}

func regexRewriteParts(rewrite string, groups int) ([]regexRewritePart, error) {
	var parts []regexRewritePart
	start := 0
	for i := 0; i < len(rewrite); {
		if rewrite[i] != '\\' || i+1 == len(rewrite) {
			i++
			continue
		}
		next := rewrite[i+1]
		if next != '\\' && (next < '0' || next > '9') {
			i++
			continue
		}
		if i > start {
			parts = append(parts, regexRewritePart{text: rewrite[start:i], group: -1})
		}
		if next == '\\' {
			parts = append(parts, regexRewritePart{text: `\`, group: -1})
			i += 2
		} else {
			end := i + 1
			for end < len(rewrite) && rewrite[end] >= '0' && rewrite[end] <= '9' {
				end++
			}
			group, err := strconv.Atoi(rewrite[i+1 : end])
			if err != nil || group > groups {
				return nil, fmt.Errorf("replace_regex references an unavailable capture group")
			}
			parts = append(parts, regexRewritePart{group: group})
			i = end
		}
		if len(parts) > maxReplacementParts {
			return nil, fmt.Errorf("replace_regex rewrite has too many parts")
		}
		start = i
	}
	if start < len(rewrite) {
		parts = append(parts, regexRewritePart{text: rewrite[start:], group: -1})
	}
	return parts, nil
}

func kqlReplaceRegex(value, lookup, rewrite any) (any, error) {
	text, ok := value.(string)
	if !ok {
		return nil, nil
	}
	pattern, ok := lookup.(string)
	if !ok {
		return nil, nil
	}
	replacement, ok := rewrite.(string)
	if !ok {
		return nil, nil
	}
	if len(pattern) > 512 {
		return nil, fmt.Errorf("replace_regex pattern exceeds 512 bytes")
	}
	if len(text) > maxReplacementBytes || len(replacement) > maxReplacementBytes {
		return nil, fmt.Errorf("replace_regex input exceeds %d bytes", maxReplacementBytes)
	}
	expression, err := compileRegexp(pattern)
	if err != nil {
		return nil, err
	}
	parts, err := regexRewriteParts(replacement, expression.NumSubexp())
	if err != nil {
		return nil, err
	}
	// Cap total capture offsets as well as matches. Many groups otherwise
	// multiply allocations even when the source string is small.
	maximum := (1 << 20) / (2 * (expression.NumSubexp() + 1))
	if maximum > maxReplacementParts {
		maximum = maxReplacementParts
	}
	matches := expression.FindAllStringSubmatchIndex(text, maximum+1)
	if len(matches) > maximum {
		return nil, fmt.Errorf("replace_regex match limit exceeded")
	}
	if int64(len(matches))*int64(len(parts)) > maxReplacementWork {
		return nil, fmt.Errorf("replace_regex work limit exceeded")
	}
	size := len(text)
	for _, match := range matches {
		size -= match[1] - match[0]
	}
	for _, match := range matches {
		for _, part := range parts {
			length := len(part.text)
			if part.group >= 0 {
				begin, end := match[2*part.group], match[2*part.group+1]
				length = 0
				if begin >= 0 {
					length = end - begin
				}
			}
			if length > maxReplacementBytes-size {
				return nil, fmt.Errorf("replace_regex result exceeds %d bytes", maxReplacementBytes)
			}
			size += length
		}
	}
	var result strings.Builder
	result.Grow(size)
	previous := 0
	for _, match := range matches {
		result.WriteString(text[previous:match[0]])
		for _, part := range parts {
			if part.group < 0 {
				result.WriteString(part.text)
			} else if begin := match[2*part.group]; begin >= 0 {
				result.WriteString(text[begin:match[2*part.group+1]])
			}
		}
		previous = match[1]
	}
	result.WriteString(text[previous:])
	return result.String(), nil
}
