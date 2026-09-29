package ksql

import (
	"sort"
	"strings"
)

func matchingColumns(columns []Column, pattern, order string) []string {
	// KQL column patterns are case-sensitive and only '*' is a wildcard.
	var matches []string
	for _, col := range columns {
		if matchColumnPattern(col.Name, pattern) {
			matches = append(matches, col.Name)
		}
	}
	if order != "" {
		sort.SliceStable(matches, func(i, j int) bool {
			comparison := strings.Compare(matches[i], matches[j])
			if strings.HasPrefix(order, "granny-") {
				comparison = compareColumnNumbers(matches[i], matches[j])
			}
			if strings.HasSuffix(order, "desc") {
				return comparison > 0
			}
			return comparison < 0
		})
	}
	return matches
}

// Compare digit runs without converting to machine integers: column names may
// contain arbitrarily large numbers. Equal runs retain their original order.
func compareColumnNumbers(a, b string) int {
	for len(a) > 0 && len(b) > 0 {
		if a[0] >= '0' && a[0] <= '9' && b[0] >= '0' && b[0] <= '9' {
			i, j := 0, 0
			for i < len(a) && a[i] >= '0' && a[i] <= '9' {
				i++
			}
			for j < len(b) && b[j] >= '0' && b[j] <= '9' {
				j++
			}
			x, y := strings.TrimLeft(a[:i], "0"), strings.TrimLeft(b[:j], "0")
			if len(x) < len(y) {
				return -1
			}
			if len(x) > len(y) {
				return 1
			}
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
			a, b = a[i:], b[j:]
		} else {
			if a[0] < b[0] {
				return -1
			}
			if a[0] > b[0] {
				return 1
			}
			a, b = a[1:], b[1:]
		}
	}
	return strings.Compare(a, b)
}

// Match only '*' and literal text, with bounded memory and no regex compilation.
func matchColumnPattern(name, pattern string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return name == pattern
	}
	if !strings.HasPrefix(name, parts[0]) {
		return false
	}
	rest := name[len(parts[0]):]
	for _, part := range parts[1 : len(parts)-1] {
		at := strings.Index(rest, part)
		if at < 0 {
			return false
		}
		rest = rest[at+len(part):]
	}
	return strings.HasSuffix(rest, parts[len(parts)-1])
}
