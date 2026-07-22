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

	"github.com/kawijayaa/striem/internal/database"
	"github.com/kawijayaa/striem/internal/eventtime"
	"github.com/tidwall/gjson"
)

const maxEventSize = 2 << 20
const maxExpandedSize = 1 << 30
const eventInsertBatchSize = 128

const (
	FormatJSON = "json"
	FormatCSV  = "csv"
)

type Mapping struct {
	Name            string            `json:"name"`
	Table           string            `json:"table"`
	Signature       string            `json:"-"`
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
	if !validTableName(mapping.Table) {
		return Result{}, errors.New("table must be a KQL identifier and cannot be Events")
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
INSERT INTO datasets(name, table_name, input_signature, source, timestamp_path, created_at)
VALUES (?, ?, ?, ?, ?, ?)`, mapping.Name, mapping.Table, mapping.Signature, mapping.Source, mapping.TimestampPath, createdAt.Format(time.RFC3339Nano))
	if err != nil {
		return Result{}, fmt.Errorf("create dataset: %w", err)
	}
	datasetID, err := result.LastInsertId()
	if err != nil {
		return Result{}, fmt.Errorf("get dataset id: %w", err)
	}

	discoveredFields := make(map[string]string)
	pendingValues := make([]any, 0, eventInsertBatchSize*8)
	pendingRecords := 0
	const insertEvents = `INSERT INTO events(dataset_id, time_generated, source, event_type, host, username, message, raw_data) VALUES `
	batchRows := make([]string, eventInsertBatchSize)
	for index := range batchRows {
		batchRows[index] = "(?, ?, ?, ?, ?, ?, ?, ?)"
	}
	eventStatement, err := tx.PrepareContext(ctx, insertEvents+strings.Join(batchRows, ","))
	if err != nil {
		return Result{}, fmt.Errorf("prepare event insert: %w", err)
	}
	defer eventStatement.Close()
	flushEvents := func() error {
		if pendingRecords == 0 {
			return nil
		}
		if pendingRecords == eventInsertBatchSize {
			if _, err := eventStatement.ExecContext(ctx, pendingValues...); err != nil {
				return err
			}
		} else {
			query := insertEvents + strings.Join(batchRows[:pendingRecords], ",")
			if _, err := tx.ExecContext(ctx, query, pendingValues...); err != nil {
				return err
			}
		}
		pendingValues = pendingValues[:0]
		pendingRecords = 0
		return nil
	}
	count, err := decodeRecords(input, format, func(index int, raw json.RawMessage) error {
		values, err := prepareEvent(raw, index, mapping, discoveredFields)
		if err != nil {
			return err
		}
		pendingValues = append(pendingValues, datasetID)
		pendingValues = append(pendingValues, values...)
		pendingRecords++
		if pendingRecords == eventInsertBatchSize {
			if err := flushEvents(); err != nil {
				return fmt.Errorf("insert records through %d: %w", index, err)
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	if count == 0 {
		return Result{}, errors.New("input contains no events")
	}
	if err := flushEvents(); err != nil {
		return Result{}, fmt.Errorf("insert records through %d: %w", count, err)
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
		Table:         mapping.Table,
		Signature:     mapping.Signature,
		Source:        mapping.Source,
		TimestampPath: mapping.TimestampPath,
		EventCount:    int64(count),
		CreatedAt:     createdAt,
	}}, nil
}

func validTableName(value string) bool {
	if value == "" || value == "Events" || !isIdentifier(value) {
		return false
	}
	return true
}

func prepareEvent(raw json.RawMessage, index int, mapping Mapping, discoveredFields map[string]string) ([]any, error) {
	if len(raw) > maxEventSize {
		return nil, fmt.Errorf("record %d exceeds the 2 MiB limit", index)
	}
	if !gjson.ValidBytes(raw) {
		return nil, fmt.Errorf("record %d must be a JSON object", index)
	}
	parsed := gjson.ParseBytes(raw)
	if !parsed.IsObject() {
		return nil, fmt.Errorf("record %d must be a JSON object", index)
	}
	if collectResultFields(parsed, "RawData", discoveredFields, 0) {
		normalized, value, err := normalizeRecord(raw)
		if err != nil {
			return nil, fmt.Errorf("normalize record %d: %w", index, err)
		}
		raw = normalized
		collectFields(value, "RawData", discoveredFields)
		parsed = gjson.ParseBytes(raw)
	}

	timestampValue := parsed.Get(mapping.TimestampPath)
	if !timestampValue.Exists() {
		return nil, fmt.Errorf("record %d has no timestamp at %q", index, mapping.TimestampPath)
	}
	timestamp, err := parseTimestamp(timestampValue, mapping.TimestampFormat)
	if err != nil {
		return nil, fmt.Errorf("record %d timestamp: %w", index, err)
	}

	source := mapping.Source
	if mapping.SourcePath != "" {
		source = valueString(parsed.Get(mapping.SourcePath))
	}
	if source == "" {
		return nil, fmt.Errorf("record %d has an empty source", index)
	}

	return []any{
		eventtime.Format(timestamp),
		source,
		mappedValue(parsed, mapping.FieldPaths, "EventType"),
		mappedValue(parsed, mapping.FieldPaths, "Host"),
		mappedValue(parsed, mapping.FieldPaths, "User"),
		mappedValue(parsed, mapping.FieldPaths, "Message"),
		string(raw),
	}, nil
}

func normalizeRecord(raw []byte) ([]byte, any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, nil, err
	}
	value, changed := normalizeEmbeddedJSON(value, 0)
	if !changed {
		return raw, value, nil
	}
	normalized, err := json.Marshal(value)
	return normalized, value, err
}

func normalizeEmbeddedJSON(value any, depth int) (any, bool) {
	if depth >= 32 {
		return value, false
	}
	switch current := value.(type) {
	case map[string]any:
		changed := false
		for key, child := range current {
			normalized, childChanged := normalizeEmbeddedJSON(child, depth+1)
			current[key] = normalized
			changed = changed || childChanged
		}
		return current, changed
	case []any:
		changed := false
		for index, child := range current {
			normalized, childChanged := normalizeEmbeddedJSON(child, depth+1)
			current[index] = normalized
			changed = changed || childChanged
		}
		return current, changed
	case string:
		trimmed := strings.TrimSpace(current)
		if len(trimmed) < 2 || (trimmed[0] != '{' && trimmed[0] != '[') || !json.Valid([]byte(trimmed)) {
			return current, false
		}
		decoder := json.NewDecoder(strings.NewReader(trimmed))
		decoder.UseNumber()
		var nested any
		if err := decoder.Decode(&nested); err != nil {
			return current, false
		}
		normalized, _ := normalizeEmbeddedJSON(nested, depth+1)
		return normalized, true
	default:
		return value, false
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

func containsEmbeddedJSON(value gjson.Result, depth int) bool {
	if depth >= 32 {
		return false
	}
	if value.Type == gjson.String {
		trimmed := strings.TrimSpace(value.String())
		return len(trimmed) >= 2 && (trimmed[0] == '{' || trimmed[0] == '[') && gjson.Valid(trimmed)
	}
	if !value.IsObject() && !value.IsArray() {
		return false
	}
	found := false
	value.ForEach(func(_, child gjson.Result) bool {
		found = containsEmbeddedJSON(child, depth+1)
		return !found
	})
	return found
}

func collectResultFields(value gjson.Result, path string, fields map[string]string, depth int) bool {
	if depth >= 32 {
		return false
	}
	embedded := false
	value.ForEach(func(key, child gjson.Result) bool {
		childPath := appendFieldPath(path, key.String())
		fields[childPath] = resultFieldType(child)
		if child.IsObject() {
			embedded = collectResultFields(child, childPath, fields, depth+1) || embedded
		} else if child.IsArray() {
			embedded = containsEmbeddedJSON(child, depth+1) || embedded
		} else if child.Type == gjson.String {
			embedded = containsEmbeddedJSON(child, depth+1) || embedded
		}
		return true
	})
	return embedded
}

func resultFieldType(value gjson.Result) string {
	switch value.Type {
	case gjson.Null:
		return "null"
	case gjson.False, gjson.True:
		return "bool"
	case gjson.Number:
		if strings.ContainsAny(value.Raw, ".eE") {
			return "real"
		}
		return "long"
	case gjson.String:
		return "string"
	case gjson.JSON:
		return "dynamic"
	default:
		return "unknown"
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
	return decodeNDJSONRecords(reader, consume)
}

func decodeNDJSONRecords(reader *bufio.Reader, consume func(int, json.RawMessage) error) (int, error) {
	count := 0
	for {
		line, err := reader.ReadBytes('\n')
		raw := bytes.TrimSpace(line)
		if len(raw) > 0 {
			count++
			if consumeErr := consume(count, raw); consumeErr != nil {
				return 0, consumeErr
			}
		}
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return 0, fmt.Errorf("read record %d: %w", count+1, err)
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

func mappedValue(record gjson.Result, paths map[string]string, field string) any {
	path := paths[field]
	if path == "" {
		return nil
	}
	value := record.Get(path)
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
