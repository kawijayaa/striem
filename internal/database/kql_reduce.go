package database

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// reduce clusters text values with Vaarandi's SLCT, which Kusto's reduce is
// based on. Values split into terms and separators; a term at a position is
// frequent when it occurs in more than threshold of the values. Each value's
// candidate pattern keeps its frequent terms and replaces the rest with *;
// candidates supported by more than threshold of the values become
// patterns, and the remaining values are counted as "others". With custom
// characters a * also absorbs the separators beside it, as Kusto's
// "foo*" for fooZ1 shows.

const (
	maxReduceValues = 1 << 18
	maxReduceBytes  = 64 << 20
	maxReduceTerms  = 256
)

type reduceAggregate struct {
	counts     map[string]int64
	order      []string
	bytes      int
	threshold  float64
	separators string
	configured bool
	// source returns [[value, Pattern], ...] for reduce kind=source.
	source bool
}

func (a *reduceAggregate) Step(value any, thresholdValue any, separatorsValue any) error {
	if !a.configured {
		a.configured, a.counts, a.threshold = true, map[string]int64{}, 0.1
		if threshold, ok := number(thresholdValue); ok {
			if threshold < 0 || threshold > 1 {
				return fmt.Errorf("reduce threshold must be between 0 and 1")
			}
			a.threshold = threshold
		}
		if text, ok := textArgument(separatorsValue); ok && !sqliteNull(separatorsValue) {
			a.separators = text
		}
	}
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) {
		return nil
	}
	if _, seen := a.counts[text]; !seen {
		if len(a.order) >= maxReduceValues || a.bytes+len(text) > maxReduceBytes {
			return fmt.Errorf("reduce input exceeds %d distinct values", maxReduceValues)
		}
		a.order = append(a.order, text)
		a.bytes += len(text)
	}
	a.counts[text]++
	return nil
}

func (a *reduceAggregate) separator(r rune) bool {
	if a.separators != "" {
		return strings.ContainsRune(a.separators, r)
	}
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// split returns the value's terms and the separator runs between them,
// starting with the (possibly empty) leading separator run.
func (a *reduceAggregate) split(text string) ([]string, []string) {
	var terms, separators []string
	var term, gap strings.Builder
	inTerm := false
	for _, r := range text {
		if a.separator(r) {
			if inTerm {
				terms = append(terms, term.String())
				term.Reset()
				inTerm = false
			}
			gap.WriteRune(r)
			continue
		}
		if !inTerm {
			separators = append(separators, gap.String())
			gap.Reset()
			inTerm = true
		}
		term.WriteRune(r)
	}
	if inTerm {
		terms = append(terms, term.String())
	}
	separators = append(separators, gap.String())
	return terms, separators
}

func (a *reduceAggregate) Done() (any, error) {
	if len(a.order) == 0 {
		return "[]", nil
	}
	type entry struct {
		terms, separators []string
	}
	entries := make([]entry, len(a.order))
	var total int64
	type position struct {
		index int
		term  string
	}
	frequency := map[position]int64{}
	for i, value := range a.order {
		terms, separators := a.split(value)
		if len(terms) > maxReduceTerms {
			terms, separators = []string{value}, []string{"", ""}
		}
		entries[i] = entry{terms, separators}
		count := a.counts[value]
		total += count
		for p, term := range terms {
			frequency[position{p, term}] += count
		}
	}
	support := a.threshold * float64(total)
	patternOf := func(item entry) (string, bool) {
		var builder strings.Builder
		frequent, wildcard := false, false
		for p := 0; p <= len(item.terms); p++ {
			if p == len(item.terms) {
				if !(wildcard && a.separators != "") {
					builder.WriteString(item.separators[p])
				}
				break
			}
			keep := float64(frequency[position{p, item.terms[p]}]) > support
			if !(a.separators != "" && (wildcard || !keep)) {
				builder.WriteString(item.separators[p])
			}
			if keep {
				builder.WriteString(item.terms[p])
				frequent, wildcard = true, false
			} else if !(wildcard && a.separators != "") {
				builder.WriteByte('*')
				wildcard = true
			}
		}
		return builder.String(), frequent
	}
	type pattern struct {
		text           string
		count          int64
		representative string
	}
	candidates := map[string]*pattern{}
	var candidateOrder []string
	assigned := make([]string, len(a.order))
	for i, value := range a.order {
		text, frequent := patternOf(entries[i])
		if !frequent {
			continue
		}
		assigned[i] = text
		if existing, ok := candidates[text]; ok {
			existing.count += a.counts[value]
			continue
		}
		candidates[text] = &pattern{text, a.counts[value], value}
		candidateOrder = append(candidateOrder, text)
	}
	var patterns []*pattern
	others := &pattern{text: "others"}
	for i, value := range a.order {
		if assigned[i] == "" || float64(candidates[assigned[i]].count) <= support {
			assigned[i] = "others"
			others.count += a.counts[value]
		}
	}
	for _, text := range candidateOrder {
		if candidate := candidates[text]; float64(candidate.count) > support {
			patterns = append(patterns, candidate)
		}
	}
	var rows [][]any
	if a.source {
		for i, value := range a.order {
			rows = append(rows, []any{value, assigned[i]})
		}
	} else {
		sort.SliceStable(patterns, func(i, j int) bool { return patterns[i].count > patterns[j].count })
		if others.count > 0 {
			patterns = append(patterns, others)
		}
		for _, pattern := range patterns {
			rows = append(rows, []any{pattern.text, pattern.count, pattern.representative})
		}
	}
	encoded, err := json.Marshal(rows)
	return string(encoded), err
}
