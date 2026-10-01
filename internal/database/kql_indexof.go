package database

import (
	"fmt"
	"unicode/utf8"
)

func kqlIndexOf(value, lookup any, options ...any) (any, error) {
	text, ok := value.(string)
	if !ok {
		return nil, nil
	}
	match, ok := lookup.(string)
	if !ok {
		return nil, nil
	}
	bounds := [3]int64{0, -1, 1}
	if len(options) > len(bounds) {
		return nil, fmt.Errorf("indexof accepts at most 5 arguments")
	}
	for i, option := range options {
		number, ok := option.(int64)
		if !ok {
			return nil, nil
		}
		bounds[i] = number
	}
	start, length, occurrence := bounds[0], bounds[1], bounds[2]
	count := int64(utf8.RuneCountInString(text))
	if start < 0 {
		start += count
	}
	if start < 0 || length < -1 || occurrence < 0 {
		return nil, nil
	}
	if occurrence == 0 {
		return nil, fmt.Errorf("indexof zero occurrence is not yet supported")
	}
	if start > count {
		return int64(-1), nil
	}
	end := count
	// Clamp before adding, including when length is MaxInt64.
	if length >= 0 && length < count-start {
		end = start + length
	}
	if match == "" {
		if occurrence-1 > count-start {
			return int64(-1), nil
		}
		return start + occurrence - 1, nil
	}
	beginByte := len(text)
	position := int64(0)
	for index := range text {
		if position == start {
			beginByte = index
		}
		position++
	}
	window := text[beginByte:]
	if len(match) > len(window) {
		return int64(-1), nil
	}
	if len(match) > maxReplacementBytes {
		return nil, fmt.Errorf("indexof match exceeds %d bytes", maxReplacementBytes)
	}
	// KMP finds overlapping occurrences in linear time. Repeated Index calls
	// can repeatedly compare a long periodic pattern at adjacent positions.
	failure := make([]int, len(match))
	for i, j := 1, 0; i < len(match); i++ {
		for j > 0 && match[i] != match[j] {
			j = failure[j-1]
		}
		if match[i] == match[j] {
			j++
		}
		failure[i] = j
	}
	matchCharacters := int64(utf8.RuneCountInString(match))
	characterPosition := int64(0)
	searchEnd := end - start
	for i, j := 0, 0; i < len(window); i++ {
		if utf8.RuneStart(window[i]) {
			characterPosition++
		}
		if characterPosition > searchEnd {
			return int64(-1), nil
		}
		for j > 0 && window[i] != match[j] {
			j = failure[j-1]
		}
		if window[i] == match[j] {
			j++
		}
		if j == len(match) {
			occurrence--
			matchPosition := characterPosition - matchCharacters
			if occurrence == 0 {
				return start + matchPosition, nil
			}
			// The documented nth-occurrence example implies a fresh length window
			// after each match, rather than one fixed window for every occurrence.
			searchStart := matchPosition + 1
			searchEnd = count - start
			if length >= 0 && length < searchEnd-searchStart {
				searchEnd = searchStart + length
			}
			j = failure[j-1]
		}
	}

	return int64(-1), nil
}
