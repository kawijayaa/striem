package ingest

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Velocidex/ordereddict"
	"github.com/kawijayaa/striem/internal/database"
	"github.com/kawijayaa/striem/internal/eventtime"
	"github.com/tidwall/gjson"
	"www.velocidex.com/golang/evtx"
)

const maxEventSize = 4 << 20
const defaultMaxExpandedSize int64 = 2 << 30
const eventInsertBatchSize = 128
const fieldDiscoverySampleRecords = 5_000

const (
	FormatJSON = "json"
	FormatCSV  = "csv"
	FormatEVTX = "evtx"
)

type Mapping struct {
	Name            string `json:"name"`
	Table           string `json:"table"`
	Signature       string `json:"-"`
	Format          string `json:"format"`
	Source          string `json:"source"`
	SourcePath      string `json:"sourcePath"`
	TimestampPath   string `json:"timestampPath"`
	TimestampFormat string `json:"timestampFormat"`
	ReplaceExisting bool   `json:"-"`
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

func (s *Service) Import(ctx context.Context, input io.Reader, compressed bool, mapping Mapping) (importResult Result, importErr error) {
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
	if format != FormatJSON && format != FormatCSV && format != FormatEVTX {
		return Result{}, fmt.Errorf("unsupported input format %q; expected json, csv, or evtx", mapping.Format)
	}
	maxInputBytes, err := configuredMaxInputBytes()
	if err != nil {
		return Result{}, err
	}

	if compressed {
		reader, err := gzip.NewReader(input)
		if err != nil {
			return Result{}, fmt.Errorf("open gzip input: %w", err)
		}
		defer reader.Close()
		input = reader
	}
	if format != FormatEVTX {
		input = &boundedReader{reader: input, remaining: maxInputBytes, limit: maxInputBytes}
	}
	s.store.MarkFullTextIndexDirty()

	connection, err := s.store.DB().Conn(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquire import connection: %w", err)
	}
	var previousSynchronous int
	if err := connection.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&previousSynchronous); err != nil {
		connection.Close()
		return Result{}, fmt.Errorf("read synchronous setting for import: %w", err)
	}
	if previousSynchronous < 0 || previousSynchronous > 3 {
		connection.Close()
		return Result{}, fmt.Errorf("unsupported SQLite synchronous setting %d", previousSynchronous)
	}
	if _, err := connection.ExecContext(ctx, "PRAGMA synchronous = OFF"); err != nil {
		connection.Close()
		return Result{}, fmt.Errorf("disable synchronous writes for import: %w", err)
	}
	var transaction *sql.Tx
	committed := false
	defer func() {
		if transaction != nil {
			_ = transaction.Rollback()
		}
		if _, err := connection.ExecContext(context.Background(), "PRAGMA synchronous = "+strconv.Itoa(previousSynchronous)); err != nil {
			if !committed {
				importErr = errors.Join(importErr, fmt.Errorf("restore synchronous writes after import: %w", err))
			}
			_ = connection.Raw(func(any) error { return driver.ErrBadConn })
		}
		if err := connection.Close(); err != nil && !committed {
			importErr = errors.Join(importErr, fmt.Errorf("release import connection: %w", err))
		}
	}()

	transaction, err = connection.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("begin import: %w", err)
	}
	if mapping.ReplaceExisting {
		if _, err := transaction.ExecContext(ctx, "DELETE FROM datasets WHERE name = ?", mapping.Name); err != nil {
			return Result{}, fmt.Errorf("replace dataset: %w", err)
		}
	}

	createdAt := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
INSERT INTO datasets(name, table_name, input_signature, source, timestamp_path, created_at)
VALUES (?, ?, ?, ?, ?, ?)`, mapping.Name, mapping.Table, mapping.Signature, mapping.Source, mapping.TimestampPath, createdAt.Format(time.RFC3339Nano))
	if err != nil {
		return Result{}, fmt.Errorf("create dataset: %w", err)
	}
	datasetID, err := result.LastInsertId()
	if err != nil {
		return Result{}, fmt.Errorf("get dataset id: %w", err)
	}

	discovery := newFieldDiscovery()
	pendingValues := make([]any, 0, eventInsertBatchSize*4)
	pendingRecords := 0
	const insertEvents = `INSERT INTO events(dataset_id, time_generated, source, raw_data) VALUES `
	batchRows := make([]string, eventInsertBatchSize)
	for index := range batchRows {
		batchRows[index] = "(?, ?, ?, ?)"
	}
	eventStatement, err := transaction.PrepareContext(ctx, insertEvents+strings.Join(batchRows, ","))
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
			if _, err := transaction.ExecContext(ctx, query, pendingValues...); err != nil {
				return err
			}
		}
		pendingValues = pendingValues[:0]
		pendingRecords = 0
		return nil
	}
	writeEvent := func(index int, values []any) error {
		pendingValues = append(pendingValues, datasetID)
		pendingValues = append(pendingValues, values...)
		pendingRecords++
		if pendingRecords == eventInsertBatchSize {
			if err := flushEvents(); err != nil {
				return fmt.Errorf("insert records through %d: %w", index, err)
			}
		}
		return nil
	}

	workerCount := min(runtime.GOMAXPROCS(0), 4)
	type prepareJob struct {
		index  int
		raw    json.RawMessage
		parsed gjson.Result
		paths  *embeddedPathNode
	}
	type prepareResult struct {
		index  int
		values []any
		err    error
	}
	workerContext, cancelWorkers := context.WithCancel(ctx)
	jobs := make(chan prepareJob, workerCount*2)
	results := make(chan prepareResult, workerCount*2)
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-workerContext.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					values, err := prepareParsedEvent(job.raw, job.parsed, job.index, mapping, job.paths, nil)
					select {
					case results <- prepareResult{index: job.index, values: values, err: err}:
					case <-workerContext.Done():
						return
					}
				}
			}
		}()
	}
	workersStopped := false
	stopWorkers := func(cancel bool) {
		if workersStopped {
			return
		}
		workersStopped = true
		if cancel {
			cancelWorkers()
		} else {
			close(jobs)
		}
		workers.Wait()
		cancelWorkers()
	}
	defer func() { stopWorkers(true) }()

	pendingJobs := 0
	nextRecord := 1
	buffered := make(map[int]prepareResult, workerCount*3)
	acceptResult := func(result prepareResult) error {
		buffered[result.index] = result
		for {
			ready, exists := buffered[nextRecord]
			if !exists {
				return nil
			}
			delete(buffered, nextRecord)
			if ready.err != nil {
				return ready.err
			}
			if err := writeEvent(ready.index, ready.values); err != nil {
				return err
			}
			nextRecord++
		}
	}
	drainOne := func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-results:
			pendingJobs--
			return acceptResult(result)
		}
	}
	drainAll := func() error {
		for pendingJobs > 0 {
			if err := drainOne(); err != nil {
				return err
			}
		}
		return nil
	}

	var decodedCount int
	var stopAfterBufferedError bool
	errPipelineStopped := errors.New("ingest pipeline stopped")
	_, decodeErr := decodeRecords(ctx, input, format, maxInputBytes, func(index int, raw json.RawMessage) error {
		decodedCount = index
		for index-nextRecord >= workerCount*3 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case result := <-results:
				pendingJobs--
				if err := acceptResult(result); err != nil {
					return err
				}
				if result.err != nil {
					stopAfterBufferedError = true
					return errPipelineStopped
				}
			}
		}
		parsed, err := parseRecord(raw, index)
		if err != nil {
			if acceptErr := acceptResult(prepareResult{index: index, err: err}); acceptErr != nil {
				return acceptErr
			}
			stopAfterBufferedError = true
			return errPipelineStopped
		}

		seenKeySet := discovery.observeTopLevelKeySet(parsed)
		fullDiscovery := index <= fieldDiscoverySampleRecords || !seenKeySet
		if fullDiscovery {
			if err := drainAll(); err != nil {
				return err
			}
			values, err := prepareParsedEvent(raw, parsed, index, mapping, discovery.embeddedPaths, discovery)
			return acceptResult(prepareResult{index: index, values: values, err: err})
		}

		job := prepareJob{index: index, raw: raw, parsed: parsed, paths: discovery.embeddedPaths}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case jobs <- job:
				pendingJobs++
				return nil
			case result := <-results:
				pendingJobs--
				if err := acceptResult(result); err != nil {
					return err
				}
				if result.err != nil {
					stopAfterBufferedError = true
					return errPipelineStopped
				}
			}
		}
	})
	if !workersStopped {
		close(jobs)
		workersStopped = true
	}
	if err := drainAll(); err != nil {
		cancelWorkers()
		workers.Wait()
		return Result{}, err
	}
	workers.Wait()
	cancelWorkers()
	if decodeErr != nil && (!errors.Is(decodeErr, errPipelineStopped) || !stopAfterBufferedError) {
		return Result{}, decodeErr
	}
	if ready, exists := buffered[nextRecord]; exists && ready.err != nil {
		return Result{}, ready.err
	}
	count := decodedCount
	if count == 0 {
		return Result{}, errors.New("input contains no events")
	}
	if err := flushEvents(); err != nil {
		return Result{}, fmt.Errorf("insert records through %d: %w", count, err)
	}
	fieldStatement, err := transaction.PrepareContext(ctx, `
INSERT OR IGNORE INTO dataset_fields(dataset_id, path, type) VALUES (?, ?, ?)`)
	if err != nil {
		return Result{}, fmt.Errorf("prepare field insert: %w", err)
	}
	defer fieldStatement.Close()
	for path, fieldType := range discovery.catalogueFields() {
		if _, err := fieldStatement.ExecContext(ctx, datasetID, path, fieldType); err != nil {
			return Result{}, fmt.Errorf("store discovered field %q: %w", path, err)
		}
	}

	if _, err := transaction.ExecContext(ctx, "UPDATE datasets SET event_count = ? WHERE id = ?", count, datasetID); err != nil {
		return Result{}, fmt.Errorf("update dataset: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Result{}, fmt.Errorf("commit import: %w", err)
	}
	committed = true
	transaction = nil

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

type embeddedPathNode struct {
	embedded bool
	objects  map[string]*embeddedPathNode
	array    *embeddedPathNode
}

func (n *embeddedPathNode) active() bool {
	return n != nil && (n.embedded || len(n.objects) != 0 || n.array != nil)
}

type fieldDiscovery struct {
	fields          map[string]string
	pathCache       map[string]map[string]string
	topLevelKeySets map[topLevelKeyFingerprint][]topLevelKeySet
	embeddedPaths   *embeddedPathNode
}

type topLevelKeyFingerprint struct {
	xor   uint64
	sum   uint64
	count int
}

type topLevelKeySet struct {
	keys  map[string]struct{}
	count int
}

func newFieldDiscovery() *fieldDiscovery {
	return &fieldDiscovery{
		fields:          make(map[string]string),
		pathCache:       make(map[string]map[string]string),
		topLevelKeySets: make(map[topLevelKeyFingerprint][]topLevelKeySet),
		embeddedPaths:   &embeddedPathNode{},
	}
}

func parseRecord(raw json.RawMessage, index int) (gjson.Result, error) {
	if len(raw) > maxEventSize {
		return gjson.Result{}, fmt.Errorf("record %d exceeds the %d MiB limit", index, maxEventSize>>20)
	}
	if !gjson.ValidBytes(raw) {
		return gjson.Result{}, fmt.Errorf("record %d must be a JSON object", index)
	}
	parsed := gjson.ParseBytes(raw)
	if !parsed.IsObject() {
		return gjson.Result{}, fmt.Errorf("record %d must be a JSON object", index)
	}
	return parsed, nil
}

func (d *fieldDiscovery) observeTopLevelKeySet(parsed gjson.Result) bool {
	fingerprint := topLevelKeyFingerprint{}
	parsed.ForEach(func(key, _ gjson.Result) bool {
		hash := uint64(14695981039346656037)
		keyString := key.String()
		for index := 0; index < len(keyString); index++ {
			hash ^= uint64(keyString[index])
			hash *= 1099511628211
		}
		fingerprint.xor ^= hash
		fingerprint.sum += hash
		fingerprint.count++
		return true
	})
	for _, candidate := range d.topLevelKeySets[fingerprint] {
		matches := true
		parsed.ForEach(func(key, _ gjson.Result) bool {
			_, matches = candidate.keys[key.String()]
			return matches
		})
		if matches && candidate.count == fingerprint.count {
			return true
		}
	}
	keys := make(map[string]struct{}, fingerprint.count)
	parsed.ForEach(func(key, _ gjson.Result) bool {
		keys[key.String()] = struct{}{}
		return true
	})
	d.topLevelKeySets[fingerprint] = append(d.topLevelKeySets[fingerprint], topLevelKeySet{keys: keys, count: fingerprint.count})
	return false
}

func prepareParsedEvent(raw json.RawMessage, parsed gjson.Result, index int, mapping Mapping, paths *embeddedPathNode, discovery *fieldDiscovery) ([]any, error) {
	var normalized []byte
	var err error
	if discovery != nil {
		if discovery.discoverResult(parsed, discovery.embeddedPaths, "", true, 0) {
			value, decodeErr := decodeJSONValue(raw)
			if decodeErr != nil {
				return nil, fmt.Errorf("normalize record %d: %w", index, decodeErr)
			}
			value, _ = normalizeKnownEmbeddedJSON(value, discovery.embeddedPaths, 0)
			normalized, err = json.Marshal(value)
		} else {
			normalized, err = compactJSON(raw)
		}
	} else if paths.active() {
		value, decodeErr := decodeJSONValue(raw)
		if decodeErr != nil {
			return nil, fmt.Errorf("normalize record %d: %w", index, decodeErr)
		}
		value, changed := normalizeKnownEmbeddedJSON(value, paths, 0)
		if changed {
			normalized, err = json.Marshal(value)
		} else {
			normalized, err = compactJSON(raw)
		}
	} else {
		normalized, err = compactJSON(raw)
	}
	if err != nil {
		return nil, fmt.Errorf("normalize record %d: %w", index, err)
	}
	if !bytes.Equal(normalized, raw) && paths.active() {
		parsed = gjson.ParseBytes(normalized)
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
		string(normalized),
	}, nil
}

func decodeJSONValue(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func compactJSON(raw []byte) ([]byte, error) {
	var output bytes.Buffer
	output.Grow(len(raw))
	err := json.Compact(&output, raw)
	return output.Bytes(), err
}

func (d *fieldDiscovery) discoverResult(value gjson.Result, node *embeddedPathNode, path string, catalogue bool, depth int) bool {
	if depth >= 32 {
		return false
	}
	found := false
	if value.IsArray() {
		arrayNode := node.array
		wasKnown := arrayNode != nil
		if arrayNode == nil {
			arrayNode = &embeddedPathNode{}
		}
		value.ForEach(func(_, child gjson.Result) bool {
			found = d.discoverResult(child, arrayNode, path, false, depth+1) || found
			return true
		})
		if arrayNode.active() && !wasKnown {
			node.array = arrayNode
		}
		return found
	}
	if value.Type == gjson.String {
		nested, ok := decodeEmbeddedJSON(value.String())
		if !ok {
			return false
		}
		node.embedded = true
		_, _ = d.discover(nested, node, path, catalogue, depth+1)
		return true
	}
	if !value.IsObject() {
		return false
	}
	value.ForEach(func(keyResult, child gjson.Result) bool {
		key := keyResult.String()
		childNode := node.objects[key]
		wasKnown := childNode != nil
		if childNode == nil {
			childNode = &embeddedPathNode{}
		}
		childPath := path
		if catalogue {
			childPath = d.cachedFieldPath(path, key)
		}
		childEmbedded := d.discoverResult(child, childNode, childPath, catalogue, depth+1)
		found = childEmbedded || found
		if catalogue {
			if childEmbedded && child.Type == gjson.String {
				d.recordField(childPath, "dynamic")
			} else {
				d.recordField(childPath, resultFieldType(child))
			}
		}
		if childNode.active() && !wasKnown {
			if node.objects == nil {
				node.objects = make(map[string]*embeddedPathNode)
			}
			node.objects[key] = childNode
		}
		return true
	})
	return found
}

func (d *fieldDiscovery) discover(value any, node *embeddedPathNode, path string, catalogue bool, depth int) (any, bool) {
	if depth >= 32 {
		return value, false
	}
	switch current := value.(type) {
	case map[string]any:
		changed := false
		for key, child := range current {
			childNode := node.objects[key]
			wasKnown := childNode != nil
			if childNode == nil {
				childNode = &embeddedPathNode{}
			}
			childPath := path
			if catalogue {
				childPath = d.cachedFieldPath(path, key)
			}
			normalized, childChanged := d.discover(child, childNode, childPath, catalogue, depth+1)
			current[key] = normalized
			changed = changed || childChanged
			if catalogue {
				d.recordField(childPath, fieldType(normalized))
			}
			if childNode.active() && !wasKnown {
				if node.objects == nil {
					node.objects = make(map[string]*embeddedPathNode)
				}
				node.objects[key] = childNode
			}
		}
		return current, changed
	case []any:
		changed := false
		arrayNode := node.array
		wasKnown := arrayNode != nil
		if arrayNode == nil {
			arrayNode = &embeddedPathNode{}
		}
		for index, child := range current {
			normalized, childChanged := d.discover(child, arrayNode, path, false, depth+1)
			current[index] = normalized
			changed = changed || childChanged
		}
		if arrayNode.active() && !wasKnown {
			node.array = arrayNode
		}
		return current, changed
	case string:
		nested, ok := decodeEmbeddedJSON(current)
		if !ok {
			return current, false
		}
		node.embedded = true
		normalized, _ := d.discover(nested, node, path, catalogue, depth+1)
		return normalized, true
	default:
		return value, false
	}
}

func (d *fieldDiscovery) recordField(path, observedType string) {
	existing, found := d.fields[path]
	if !found || existing == "null" || existing == "unknown" {
		d.fields[path] = observedType
		return
	}
	if observedType == "null" || observedType == "unknown" || observedType == existing {
		return
	}
	d.fields[path] = "mixed"
}

func (d *fieldDiscovery) catalogueFields() map[string]string {
	rootNames := make(map[string][]string)
	for path := range d.fields {
		if isIdentifier(path) {
			key := strings.ToLower(path)
			rootNames[key] = append(rootNames[key], path)
		}
	}

	blockedRoots := make(map[string]struct{})
	for key, names := range rootNames {
		if key == "timegenerated" || key == "source" || key == "rawdata" || len(names) > 1 {
			for _, name := range names {
				blockedRoots[name] = struct{}{}
			}
		}
	}

	result := make(map[string]string, len(d.fields))
	for path, fieldType := range d.fields {
		cataloguePath := path
		for root := range blockedRoots {
			if path == root || strings.HasPrefix(path, root+".") || strings.HasPrefix(path, root+"[") {
				cataloguePath = "RawData[" + strconv.Quote(root) + "]" + strings.TrimPrefix(path, root)
				break
			}
		}
		result[cataloguePath] = fieldType
	}
	return result
}

func normalizeKnownEmbeddedJSON(value any, node *embeddedPathNode, depth int) (any, bool) {
	if depth >= 32 {
		return value, false
	}
	changed := false
	if node.embedded {
		if current, ok := value.(string); ok {
			if nested, valid := decodeEmbeddedJSON(current); valid {
				value = nested
				changed = true
			}
		}
	}
	switch current := value.(type) {
	case map[string]any:
		for key, childNode := range node.objects {
			child, exists := current[key]
			if !exists {
				continue
			}
			normalized, childChanged := normalizeKnownEmbeddedJSON(child, childNode, depth+1)
			current[key] = normalized
			changed = changed || childChanged
		}
	case []any:
		if node.array != nil {
			for index, child := range current {
				normalized, childChanged := normalizeKnownEmbeddedJSON(child, node.array, depth+1)
				current[index] = normalized
				changed = changed || childChanged
			}
		}
	}
	return value, changed
}

func decodeEmbeddedJSON(value string) (any, bool) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 || (trimmed[0] != '{' && trimmed[0] != '[') || !json.Valid([]byte(trimmed)) {
		return nil, false
	}
	nested, err := decodeJSONValue([]byte(trimmed))
	return nested, err == nil
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

func (d *fieldDiscovery) cachedFieldPath(path, key string) string {
	children := d.pathCache[path]
	if children == nil {
		children = make(map[string]string)
		d.pathCache[path] = children
	}
	if cached, exists := children[key]; exists {
		return cached
	}
	childPath := appendFieldPath(path, key)
	children[key] = childPath
	return childPath
}

func appendFieldPath(path, key string) string {
	if path == "" {
		if isIdentifier(key) {
			return key
		}
		return "RawData[" + strconv.Quote(key) + "]"
	}
	if isIdentifier(key) {
		return path + "." + key
	}
	return path + "[" + strconv.Quote(key) + "]"
}

func isIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if character != '_' && !unicode.IsLetter(character) && (index == 0 || !unicode.IsDigit(character)) {
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
	limit     int64
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
		return 0, fmt.Errorf("expanded input exceeds the %d byte limit", r.limit)
	}
	return 0, err
}

func configuredMaxInputBytes() (int64, error) {
	value := strings.TrimSpace(os.Getenv("STRIEM_MAX_INPUT_BYTES"))
	if value == "" {
		return defaultMaxExpandedSize, nil
	}
	limit, err := strconv.ParseInt(value, 10, 64)
	if err != nil || limit <= 0 {
		return 0, fmt.Errorf("STRIEM_MAX_INPUT_BYTES must be a positive integer number of bytes, got %q", value)
	}
	return limit, nil
}

func decodeRecords(ctx context.Context, input io.Reader, format string, maxInputBytes int64, consume func(int, json.RawMessage) error) (int, error) {
	switch format {
	case FormatCSV:
		return decodeCSVRecords(ctx, input, consume)
	case FormatEVTX:
		return decodeEVTXRecords(ctx, input, maxInputBytes, consume)
	default:
		return decodeJSONRecords(ctx, input, consume)
	}
}

func decodeEVTXRecords(ctx context.Context, input io.Reader, maxInputBytes int64, consume func(int, json.RawMessage) error) (count int, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			count = 0
			err = fmt.Errorf("decode EVTX input: %v", recovered)
		}
	}()

	seeker, cleanup, err := evtxReadSeeker(input, maxInputBytes)
	if err != nil {
		return 0, err
	}
	defer cleanup()

	chunks, err := evtx.GetChunks(seeker)
	if err != nil {
		return 0, fmt.Errorf("decode EVTX header: %w", err)
	}
	for _, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if chunk.Header.LastEventRecNumber < chunk.Header.FirstEventRecNumber ||
			chunk.Header.LastEventRecNumber-chunk.Header.FirstEventRecNumber >= evtx.EVTX_CHUNK_SIZE/evtx.EVTX_EVENT_RECORD_SIZE {
			return 0, fmt.Errorf("decode EVTX chunk at offset %d: invalid record range", chunk.Offset)
		}
		records, err := chunk.Parse(0)
		if err != nil {
			return 0, fmt.Errorf("decode EVTX chunk at offset %d: %w", chunk.Offset, err)
		}
		for _, record := range records {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			encoded, err := marshalEVTXEvent(record.Event, count+1)
			if err != nil {
				return 0, err
			}
			count++
			if err := consume(count, encoded); err != nil {
				return 0, err
			}
		}
	}
	return count, nil
}

func marshalEVTXEvent(value any, index int) ([]byte, error) {
	eventMap, ok := value.(*ordereddict.Dict)
	if !ok {
		return nil, fmt.Errorf("decode EVTX record %d: event payload has type %T, want *ordereddict.Dict", index, value)
	}
	event, ok := eventMap.Get("Event")
	if !ok {
		return nil, fmt.Errorf("decode EVTX record %d: event payload has no Event value", index)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("encode EVTX record %d: %w", index, err)
	}
	if !gjson.ParseBytes(encoded).IsObject() {
		return nil, fmt.Errorf("decode EVTX record %d: event payload is not an object", index)
	}
	return encoded, nil
}

func evtxReadSeeker(input io.Reader, maxInputBytes int64) (io.ReadSeeker, func(), error) {
	if seeker, ok := input.(io.ReadSeeker); ok {
		size, err := seeker.Seek(0, io.SeekEnd)
		if err != nil {
			return nil, func() {}, fmt.Errorf("measure EVTX input: %w", err)
		}
		if size > maxInputBytes {
			return nil, func() {}, fmt.Errorf("expanded input exceeds the %d byte limit", maxInputBytes)
		}
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return nil, func() {}, fmt.Errorf("rewind EVTX input: %w", err)
		}
		return &boundedReadSeeker{reader: seeker, limit: size}, func() {}, nil
	}

	temporary, err := os.CreateTemp(strings.TrimSpace(os.Getenv("STRIEM_DATA_DIR")), ".striem-evtx-*")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create EVTX temporary file: %w", err)
	}
	cleanup := func() {
		name := temporary.Name()
		_ = temporary.Close()
		_ = os.Remove(name)
	}
	limited := &boundedReader{reader: input, remaining: maxInputBytes, limit: maxInputBytes}
	if _, err := io.Copy(temporary, limited); err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("buffer EVTX input: %w", err)
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("rewind EVTX temporary file: %w", err)
	}
	return temporary, cleanup, nil
}

type boundedReadSeeker struct {
	reader io.ReadSeeker
	limit  int64
	offset int64
}

func (reader *boundedReadSeeker) Read(buffer []byte) (int, error) {
	if reader.offset >= reader.limit {
		return 0, io.EOF
	}
	if remaining := reader.limit - reader.offset; int64(len(buffer)) > remaining {
		buffer = buffer[:remaining]
	}
	count, err := reader.reader.Read(buffer)
	reader.offset += int64(count)
	return count, err
}

func (reader *boundedReadSeeker) Seek(offset int64, whence int) (int64, error) {
	position, err := reader.reader.Seek(offset, whence)
	if err != nil {
		return 0, err
	}
	if position < 0 || position > reader.limit {
		return 0, fmt.Errorf("EVTX seek offset %d exceeds the measured %d byte input", position, reader.limit)
	}
	reader.offset = position
	return position, nil
}

func decodeCSVRecords(ctx context.Context, input io.Reader, consume func(int, json.RawMessage) error) (int, error) {
	buffered := bufio.NewReader(input)
	if prefix, _ := buffered.Peek(3); bytes.Equal(prefix, []byte{0xEF, 0xBB, 0xBF}) {
		_, _ = buffered.Discard(3)
	}
	reader := csv.NewReader(buffered)
	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read CSV header: %w", err)
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
	keyFragments := make([][]byte, len(header))
	for index, name := range header {
		fragment := make([]byte, 0, len(name)+4)
		if index != 0 {
			fragment = append(fragment, ',')
		}
		fragment = appendJSONString(fragment, name)
		fragment = append(fragment, ':')
		keyFragments[index] = fragment
	}

	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return 0, fmt.Errorf("decode CSV record %d: %w", count+1, err)
		}
		raw := make([]byte, 1, len(header)*16)
		raw[0] = '{'
		for index, value := range record {
			raw = append(raw, keyFragments[index]...)
			raw = appendJSONString(raw, value)
		}
		raw = append(raw, '}')
		count++
		if err := consume(count, raw); err != nil {
			return 0, err
		}
	}
}

func appendJSONString(output []byte, value string) []byte {
	const hexadecimal = "0123456789abcdef"
	output = append(output, '"')
	start := 0
	for index := 0; index < len(value); {
		if character := value[index]; character < utf8.RuneSelf {
			if character >= 0x20 && character != '\\' && character != '"' && character != '<' && character != '>' && character != '&' {
				index++
				continue
			}
			output = append(output, value[start:index]...)
			switch character {
			case '\\', '"':
				output = append(output, '\\', character)
			case '\n':
				output = append(output, '\\', 'n')
			case '\r':
				output = append(output, '\\', 'r')
			case '\t':
				output = append(output, '\\', 't')
			default:
				output = append(output, '\\', 'u', '0', '0', hexadecimal[character>>4], hexadecimal[character&0x0f])
			}
			index++
			start = index
			continue
		}
		runeValue, size := utf8.DecodeRuneInString(value[index:])
		if runeValue == utf8.RuneError && size == 1 {
			output = append(output, value[start:index]...)
			output = append(output, "\\ufffd"...)
			index++
			start = index
			continue
		}
		if runeValue == '\u2028' || runeValue == '\u2029' {
			output = append(output, value[start:index]...)
			output = append(output, '\\', 'u', '2', '0', '2', hexadecimal[runeValue&0x0f])
			index += size
			start = index
			continue
		}
		index += size
	}
	output = append(output, value[start:]...)
	return append(output, '"')
}

func decodeJSONRecords(ctx context.Context, input io.Reader, consume func(int, json.RawMessage) error) (int, error) {
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
			if err := ctx.Err(); err != nil {
				return 0, err
			}
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
	return decodeNDJSONRecords(ctx, reader, consume)
}

func decodeNDJSONRecords(ctx context.Context, reader *bufio.Reader, consume func(int, json.RawMessage) error) (int, error) {
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		line, err := reader.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("read record %d: %w", count+1, err)
		}
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
