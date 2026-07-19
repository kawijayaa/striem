package ingest

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/oka/striem/internal/database"
	"github.com/oka/striem/internal/eventtime"
	"github.com/tidwall/gjson"
)

const maxEventSize = 2 << 20
const maxExpandedSize = 128 << 20

const (
	FormatJSON = "json"
	FormatCSV  = "csv"
)

type Mapping struct {
	Name            string            `json:"name"`
	Format          string            `json:"format"`
	Source          string            `json:"source"`
	SourcePath      string            `json:"sourcePath"`
	TimestampPath   string            `json:"timestampPath"`
	TimestampFormat string            `json:"timestampFormat"`
	FieldPaths      map[string]string `json:"fieldPaths"`
	ReplaceExisting bool              `json:"-"`
}

type Result struct {
	Dataset database.Dataset `json:"dataset"`
}

type Service struct {
	store *database.Store
}

func New(store *database.Store) *Service {
	return &Service{store: store}
}

func (s *Service) Import(ctx context.Context, input io.Reader, compressed bool, mapping Mapping) (Result, error) {
	if strings.TrimSpace(mapping.Name) == "" {
		return Result{}, errors.New("dataset name is required")
	}
	if strings.TrimSpace(mapping.TimestampPath) == "" {
		return Result{}, errors.New("timestampPath is required")
	}
	if mapping.Source == "" && mapping.SourcePath == "" {
		return Result{}, errors.New("source or sourcePath is required")
	}
	format := strings.ToLower(strings.TrimSpace(mapping.Format))
	if format == "" {
		format = FormatJSON
	}
	if format != FormatJSON && format != FormatCSV {
		return Result{}, fmt.Errorf("unsupported input format %q; expected json or csv", mapping.Format)
	}

	if compressed {
		reader, err := gzip.NewReader(input)
		if err != nil {
			return Result{}, fmt.Errorf("open gzip input: %w", err)
		}
		defer reader.Close()
		input = reader
	}
	input = &boundedReader{reader: input, remaining: maxExpandedSize}

	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("begin import: %w", err)
	}
	defer tx.Rollback()
	if mapping.ReplaceExisting {
		if _, err := tx.ExecContext(ctx, "DELETE FROM datasets WHERE name = ?", mapping.Name); err != nil {
			return Result{}, fmt.Errorf("replace dataset: %w", err)
		}
	}

	createdAt := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
INSERT INTO datasets(name, source, timestamp_path, created_at)
VALUES (?, ?, ?, ?)`, mapping.Name, mapping.Source, mapping.TimestampPath, createdAt.Format(time.RFC3339Nano))
	if err != nil {
		return Result{}, fmt.Errorf("create dataset: %w", err)
	}
	datasetID, err := result.LastInsertId()
	if err != nil {
		return Result{}, fmt.Errorf("get dataset id: %w", err)
	}

	statement, err := tx.PrepareContext(ctx, `
INSERT INTO events(dataset_id, time_generated, source, event_type, host, username, message, raw_data)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return Result{}, fmt.Errorf("prepare event insert: %w", err)
	}
	defer statement.Close()
	discoveredFields := make(map[string]string)

	count, err := decodeRecords(input, format, func(index int, raw json.RawMessage) error {
		if len(raw) > maxEventSize {
			return fmt.Errorf("record %d exceeds the 2 MiB limit", index)
		}
		if !gjson.ValidBytes(raw) || !gjson.ParseBytes(raw).IsObject() {
			return fmt.Errorf("record %d must be a JSON object", index)
		}
		normalized, value, err := normalizeRecord(raw)
		if err != nil {
			return fmt.Errorf("normalize record %d: %w", index, err)
		}
		raw = normalized
		collectFields(value, "RawData", discoveredFields)

		timestampValue := gjson.GetBytes(raw, mapping.TimestampPath)
		if !timestampValue.Exists() {
			return fmt.Errorf("record %d has no timestamp at %q", index, mapping.TimestampPath)
		}
		timestamp, err := parseTimestamp(timestampValue, mapping.TimestampFormat)
		if err != nil {
			return fmt.Errorf("record %d timestamp: %w", index, err)
		}

		source := mapping.Source
		if mapping.SourcePath != "" {
			source = valueString(gjson.GetBytes(raw, mapping.SourcePath))
		}
		if source == "" {
			return fmt.Errorf("record %d has an empty source", index)
		}

		_, err = statement.ExecContext(ctx,
			datasetID,
			eventtime.Format(timestamp),
			source,
			mappedValue(raw, mapping.FieldPaths, "EventType"),
			mappedValue(raw, mapping.FieldPaths, "Host"),
			mappedValue(raw, mapping.FieldPaths, "User"),
			mappedValue(raw, mapping.FieldPaths, "Message"),
			string(raw),
		)
		if err != nil {
			return fmt.Errorf("insert record %d: %w", index, err)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	if count == 0 {
		return Result{}, errors.New("input contains no events")
	}
	fieldStatement, err := tx.PrepareContext(ctx, `
INSERT OR IGNORE INTO dataset_fields(dataset_id, path, type) VALUES (?, ?, ?)`)
	if err != nil {
		return Result{}, fmt.Errorf("prepare field insert: %w", err)
	}
	defer fieldStatement.Close()
	for path, fieldType := range discoveredFields {
		if _, err := fieldStatement.ExecContext(ctx, datasetID, path, fieldType); err != nil {
			return Result{}, fmt.Errorf("store discovered field %q: %w", path, err)
		}
	}

	if _, err := tx.ExecContext(ctx, "UPDATE datasets SET event_count = ? WHERE id = ?", count, datasetID); err != nil {
		return Result{}, fmt.Errorf("update dataset: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("commit import: %w", err)
	}

	return Result{Dataset: database.Dataset{
		ID:            datasetID,
		Name:          mapping.Name,
		Source:        mapping.Source,
		TimestampPath: mapping.TimestampPath,
		EventCount:    int64(count),
		CreatedAt:     createdAt,
	}}, nil
}

func normalizeRecord(raw []byte) ([]byte, any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, nil, err
	}
	value = normalizeEmbeddedJSON(value, 0)
	normalized, err := json.Marshal(value)
	return normalized, value, err
}

func normalizeEmbeddedJSON(value any, depth int) any {
	if depth >= 32 {
		return value
	}
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			current[key] = normalizeEmbeddedJSON(child, depth+1)
		}
		return current
	case []any:
		for index, child := range current {
			current[index] = normalizeEmbeddedJSON(child, depth+1)
		}
		return current
	case string:
		trimmed := strings.TrimSpace(current)
		if len(trimmed) < 2 || (trimmed[0] != '{' && trimmed[0] != '[') || !json.Valid([]byte(trimmed)) {
			return current
		}
		decoder := json.NewDecoder(strings.NewReader(trimmed))
		decoder.UseNumber()
		var nested any
		if err := decoder.Decode(&nested); err != nil {
			return current
		}
		return normalizeEmbeddedJSON(nested, depth+1)
	default:
		return value
	}
}

func collectFields(value any, path string, fields map[string]string) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	for key, child := range object {
		childPath := appendFieldPath(path, key)
		fields[childPath] = fieldType(child)
		if _, nested := child.(map[string]any); nested {
			collectFields(child, childPath, fields)
		}
	}
}

func appendFieldPath(path, key string) string {
	if isIdentifier(key) {
		return path + "." + key
	}
	return path + "[" + strconv.Quote(key) + "]"
}

func isIdentifier(value string) bool {
	if value == "" || !((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z') || value[0] == '_') {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if !((character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_') {
			return false
		}
	}
	return true
}

func fieldType(value any) string {
	switch current := value.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case json.Number:
		if strings.ContainsAny(current.String(), ".eE") {
			return "real"
		}
		return "long"
	case string:
		return "string"
	case map[string]any, []any:
		return "dynamic"
	default:
		return "unknown"
	}
}

type boundedReader struct {
	reader    io.Reader
	remaining int64
	checked   bool
}

func (r *boundedReader) Read(buffer []byte) (int, error) {
	if r.remaining > 0 {
		if int64(len(buffer)) > r.remaining {
			buffer = buffer[:r.remaining]
		}
		count, err := r.reader.Read(buffer)
		r.remaining -= int64(count)
		return count, err
	}
	if r.checked {
		return 0, io.EOF
	}
	r.checked = true
	var probe [1]byte
	count, err := r.reader.Read(probe[:])
	if count > 0 {
		return 0, fmt.Errorf("expanded input exceeds the %d MiB limit", maxExpandedSize>>20)
	}
	return 0, err
}

func decodeRecords(input io.Reader, format string, consume func(int, json.RawMessage) error) (int, error) {
	if format == FormatCSV {
		return decodeCSVRecords(input, consume)
	}
	return decodeJSONRecords(input, consume)
}

func decodeCSVRecords(input io.Reader, consume func(int, json.RawMessage) error) (int, error) {
	reader := csv.NewReader(input)
	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read CSV header: %w", err)
	}
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\uFEFF")
	}
	seen := make(map[string]struct{}, len(header))
	for index, name := range header {
		if strings.TrimSpace(name) == "" {
			return 0, fmt.Errorf("CSV header column %d is empty", index+1)
		}
		if _, exists := seen[name]; exists {
			return 0, fmt.Errorf("CSV header %q appears more than once", name)
		}
		seen[name] = struct{}{}
	}

	count := 0
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return 0, fmt.Errorf("decode CSV record %d: %w", count+1, err)
		}
		value := make(map[string]string, len(header))
		for index, name := range header {
			value[name] = record[index]
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return 0, fmt.Errorf("encode CSV record %d: %w", count+1, err)
		}
		count++
		if err := consume(count, raw); err != nil {
			return 0, err
		}
	}
}

func decodeJSONRecords(input io.Reader, consume func(int, json.RawMessage) error) (int, error) {
	reader := bufio.NewReader(input)
	first, err := firstNonWhitespace(reader)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 0, nil
		}
		return 0, err
	}

	decoder := json.NewDecoder(reader)
	count := 0
	if first == '[' {
		if _, err := decoder.Token(); err != nil {
			return 0, fmt.Errorf("read JSON array: %w", err)
		}
		for decoder.More() {
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				return 0, fmt.Errorf("decode record %d: %w", count+1, err)
			}
			count++
			if err := consume(count, raw); err != nil {
				return 0, err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return 0, fmt.Errorf("close JSON array: %w", err)
		}
		return count, ensureEOF(decoder)
	}

	for {
		var raw json.RawMessage
		err := decoder.Decode(&raw)
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return 0, fmt.Errorf("decode record %d: %w", count+1, err)
		}
		count++
		if err := consume(count, raw); err != nil {
			return 0, err
		}
	}
}

func firstNonWhitespace(reader *bufio.Reader) (byte, error) {
	for {
		value, err := reader.Peek(1)
		if err != nil {
			return 0, err
		}
		if !bytes.ContainsRune([]byte(" \t\r\n"), rune(value[0])) {
			return value[0], nil
		}
		reader.ReadByte()
	}
}

func ensureEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("unexpected JSON after array")
	}
	return err
}

func mappedValue(raw []byte, paths map[string]string, field string) any {
	path := paths[field]
	if path == "" {
		return nil
	}
	value := gjson.GetBytes(raw, path)
	if !value.Exists() || value.Type == gjson.Null {
		return nil
	}
	return valueString(value)
}

func valueString(value gjson.Result) string {
	if value.Type == gjson.String {
		return value.String()
	}
	return value.Raw
}

func parseTimestamp(value gjson.Result, format string) (time.Time, error) {
	format = strings.TrimSpace(format)
	if format == "" || strings.EqualFold(format, "auto") {
		if value.Type == gjson.Number {
			return unixTimestamp(value.Float()), nil
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, value.String()); err == nil {
				return parsed, nil
			}
		}
		return time.Time{}, fmt.Errorf("%q is not RFC3339 or Unix time", value.String())
	}

	switch strings.ToLower(format) {
	case "rfc3339":
		return time.Parse(time.RFC3339Nano, value.String())
	case "unix":
		seconds, err := strconv.ParseFloat(value.String(), 64)
		if err != nil {
			return time.Time{}, err
		}
		return time.Unix(int64(seconds), int64((seconds-float64(int64(seconds)))*1e9)), nil
	case "unix_ms":
		milliseconds, err := strconv.ParseInt(value.String(), 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		return time.UnixMilli(milliseconds), nil
	default:
		return time.Parse(format, value.String())
	}
}

func unixTimestamp(value float64) time.Time {
	if value > 1e12 {
		return time.UnixMilli(int64(value))
	}
	seconds := int64(value)
	return time.Unix(seconds, int64((value-float64(seconds))*1e9))
}
