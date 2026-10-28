package database

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxCharacterStringBytes = 4 << 20
const maxTranslationAlphabetBytes = 64 << 10

func kqlTranslate(searchValue, replacementValue, sourceValue any) (any, error) {
	search, ok := searchValue.(string)
	replacement, valid := replacementValue.(string)
	source, sourceOK := sourceValue.(string)
	if !ok || !valid || !sourceOK {
		return nil, nil
	}
	if len(search) > maxTranslationAlphabetBytes || len(replacement) > maxTranslationAlphabetBytes || len(source) > maxCharacterStringBytes {
		return nil, fmt.Errorf("translate input exceeds byte limit")
	}
	if !utf8.ValidString(search) || !utf8.ValidString(replacement) || !utf8.ValidString(source) {
		return nil, nil
	}
	searchPoints, replacementPoints := []rune(search), []rune(replacement)
	if len(searchPoints) == 0 {
		return source, nil
	}
	if len(replacementPoints) > 1 && len(replacementPoints) != len(searchPoints) {
		return nil, fmt.Errorf("translate unequal multi-character lists require engine verification")
	}
	table := make(map[rune]rune, len(searchPoints))
	for i, point := range searchPoints {
		if _, seen := table[point]; seen {
			continue
		}
		if len(replacementPoints) == 0 {
			table[point] = -1
		} else if len(replacementPoints) == 1 {
			table[point] = replacementPoints[0]
		} else {
			table[point] = replacementPoints[i]
		}
	}
	var output strings.Builder
	for _, point := range source {
		mapped, found := table[point]
		if found {
			point = mapped
		}
		if point < 0 {
			continue
		}
		if output.Len()+utf8.RuneLen(point) > maxCharacterStringBytes {
			return nil, fmt.Errorf("translate output exceeds byte limit")
		}
		output.WriteRune(point)
	}
	return output.String(), nil
}

func kqlReverse(value any, kind string) (any, error) {
	converted, err := kqlJoinScalarText(value, kind)
	if err != nil {
		return nil, err
	}
	text, ok := converted.(string)
	if !ok {
		return nil, nil
	}

	if len(text) > maxCharacterStringBytes {
		return nil, fmt.Errorf("reverse input exceeds byte limit")
	}
	if !utf8.ValidString(text) {
		return nil, nil
	}
	points := []rune(text)
	for left, right := 0, len(points)-1; left < right; left, right = left+1, right-1 {
		points[left], points[right] = points[right], points[left]
	}
	return string(points), nil
}
