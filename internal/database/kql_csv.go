package database

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

func kqlParseCSV(value any) (any, error) {
	if sqliteNull(value) {
		return nil, nil
	}
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("parse_csv requires string input")
	}
	if len(text) > maxDynamicInputBytes {
		return nil, fmt.Errorf("parse_csv input exceeds %d bytes", maxDynamicInputBytes)
	}
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("parse_csv input must be valid UTF-8")
	}
	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = -1
	fields, err := reader.Read()
	if err == io.EOF {
		fields = []string{""}
	} else if err != nil {
		// A partial record must not look like a successfully parsed array.
		return nil, nil
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxReplacementBytes {
		return nil, fmt.Errorf("parse_csv encoded array exceeds %d bytes", maxReplacementBytes)
	}
	return string(encoded), nil
}
