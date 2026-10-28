package database

import (
	"encoding/json"
	"strconv"
	"strings"
)

func validOctets(parts []string) bool {
	for _, part := range parts {
		if part == "" || len(part) > 3 {
			return false
		}
		value, err := strconv.Atoi(part)
		if err != nil || value > 255 {
			return false
		}
	}
	return true
}

func validIPv4(text string) bool {
	parts := strings.Split(text, ".")
	return len(parts) == 4 && validOctets(parts)
}

// validIPv4Prefix accepts a complete address or 1-3 octets ending with a dot.
func validIPv4Prefix(text string) bool {
	if validIPv4(text) {
		return true
	}
	if !strings.HasSuffix(text, ".") {
		return false
	}
	parts := strings.Split(strings.TrimSuffix(text, "."), ".")
	return len(parts) >= 1 && len(parts) <= 3 && validOctets(parts)
}

func isAlphaNumeric(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// ipv4Tokens lists the valid IPv4 addresses in text that are bounded by
// characters other than letters, digits and dots.
func ipv4Tokens(text string) []string {
	var tokens []string
	for i := 0; i < len(text); {
		if !(text[i] >= '0' && text[i] <= '9') || (i > 0 && (isAlphaNumeric(text[i-1]) || text[i-1] == '.')) {
			i++
			continue
		}
		j := i
		for j < len(text) && (text[j] >= '0' && text[j] <= '9' || text[j] == '.') {
			j++
		}
		// A trailing dot ends a sentence rather than the address.
		candidate := strings.TrimRight(text[i:j], ".")
		if (j == len(text) || !isAlphaNumeric(text[j])) && validIPv4(candidate) {
			tokens = append(tokens, candidate)
		}
		i = j
	}
	return tokens
}

// ipv4Targets reads string arguments or dynamic arrays of strings.
func ipv4Targets(values []any) []string {
	var targets []string
	for _, value := range values {
		text, ok := textArgument(value)
		if !ok || sqliteNull(value) {
			continue
		}
		var list []any
		if strings.HasPrefix(strings.TrimSpace(text), "[") && json.Unmarshal([]byte(text), &list) == nil {
			for _, item := range list {
				if s, ok := item.(string); ok {
					targets = append(targets, s)
				}
			}
			continue
		}
		targets = append(targets, text)
	}
	return targets
}

// kqlHasIPv4 implements has_ipv4, has_ipv4_prefix, has_any_ipv4 and
// has_any_ipv4_prefix.
func kqlHasIPv4(prefix bool, sourceValue any, values ...any) any {
	source, ok := textArgument(sourceValue)
	if !ok || sqliteNull(sourceValue) || len(source) > maxReplacementBytes {
		return int64(0)
	}
	targets := ipv4Targets(values)
	if len(targets) > 1024 {
		return nil
	}
	tokens := ipv4Tokens(source)
	for _, target := range targets {
		if prefix && !validIPv4Prefix(target) || !prefix && !validIPv4(target) {
			continue
		}
		for _, token := range tokens {
			if token == target || prefix && strings.HasSuffix(target, ".") && strings.HasPrefix(token, target) {
				return int64(1)
			}
		}
	}
	return int64(0)
}
