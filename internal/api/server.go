package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/kawijayaa/striem/internal/database"
	"github.com/kawijayaa/striem/internal/kql"
	webassets "github.com/kawijayaa/striem/web"
)

type Server struct {
	store         *database.Store
	logger        *slog.Logger
	mu            sync.RWMutex
	tableCatalog  kql.TableCatalog
	ready         bool
	startupError  string
	challengeName string
	prepareQuery  func(context.Context, string) (*sql.Stmt, error)
}

func New(store *database.Store, logger *slog.Logger) *Server {
	tableCatalog, err := buildTableCatalog(context.Background(), store)
	if err != nil {
		panic(err)
	}
	challengeName, err := store.ChallengeName(context.Background())
	if err != nil {
		panic(err)
	}
	return &Server{
		store:         store,
		logger:        logger,
		tableCatalog:  tableCatalog,
		ready:         true,
		challengeName: challengeName,
		prepareQuery:  store.DB().PrepareContext,
	}
}

// SetChallengeName makes the deployment name available to the startup screen.
func (s *Server) SetChallengeName(name string) {
	s.mu.Lock()
	s.challengeName = name
	s.mu.Unlock()
}

func buildTableCatalog(ctx context.Context, store *database.Store) (kql.TableCatalog, error) {
	datasets, err := store.ListDatasets(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := store.ListFieldGroups(ctx)
	if err != nil {
		return nil, err
	}
	fieldsByTable := make(map[string][]kql.Field, len(groups))
	for _, group := range groups {
		fieldsByTable[group.Table] = logicalFields(group.Fields)
	}
	tableCatalog := make(kql.TableCatalog, len(datasets))
	for _, dataset := range datasets {
		if dataset.Table != "" {
			table := tableCatalog[dataset.Table]
			table.IDs = append(table.IDs, dataset.ID)
			table.Fields = fieldsByTable[dataset.Table]
			tableCatalog[dataset.Table] = table
		}
	}
	return tableCatalog, nil
}

// SetLoading keeps the static interface online while deployment data is ingested.
func (s *Server) SetLoading() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = false
	s.startupError = ""
}

// SetStartupError makes readiness failures visible without taking down the web server.
func (s *Server) SetStartupError(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = false
	s.startupError = message
}

// RefreshCatalog publishes the datasets and fields added during ingestion.
func (s *Server) RefreshCatalog(ctx context.Context) error {
	catalog, err := buildTableCatalog(ctx, s.store)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.tableCatalog = catalog
	s.ready = true
	s.startupError = ""
	s.mu.Unlock()
	return nil
}

func logicalFields(fields []database.Field) []kql.Field {
	reserved := map[string]struct{}{"timegenerated": {}, "source": {}, "rawdata": {}}
	result := make([]kql.Field, 0, len(fields))
	indexes := make(map[string]int)
	ambiguous := make(map[string]struct{})
	for _, field := range fields {
		if !isLogicalFieldName(field.Path) {
			continue
		}
		key := strings.ToLower(field.Path)
		if _, found := reserved[key]; found {
			continue
		}
		if _, found := ambiguous[key]; found {
			continue
		}
		if index, found := indexes[key]; found {
			result[index].Name = ""
			ambiguous[key] = struct{}{}
			continue
		}
		indexes[key] = len(result)
		result = append(result, kql.Field{Name: field.Path, Type: field.Type})
	}
	filtered := result[:0]
	for _, field := range result {
		if field.Name != "" {
			filtered = append(filtered, field)
		}
	}
	return filtered
}

func isLogicalFieldName(value string) bool {
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

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/ready", s.readiness)
	mux.HandleFunc("GET /api/schema", s.whenReady(s.schema))
	mux.HandleFunc("GET /api/fields", s.whenReady(s.fields))
	mux.HandleFunc("GET /api/questions", s.whenReady(s.questions))
	mux.HandleFunc("POST /api/questions/{id}/answer", s.whenReady(requireSameOrigin(s.submitAnswer)))
	mux.HandleFunc("POST /api/query", s.whenReady(requireSameOrigin(s.query)))
	mux.HandleFunc("POST /api/query/validate", s.whenReady(requireSameOrigin(s.validateQuery)))

	static, err := fs.Sub(webassets.Files, "dist")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServerFS(static))
	return s.logRequests(mux)
}

func (s *Server) fields(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.ListFieldGroups(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list available fields", nil)
		return
	}
	common := []database.Field{
		{Path: "TimeGenerated", Type: "datetime"},
		{Path: "Source", Type: "string"},
		{Path: "RawData", Type: "dynamic"},
	}
	writeJSON(w, http.StatusOK, map[string]any{"common": common, "tables": groups})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := s.store.DB().PingContext(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.mu.RLock()
	ready, startupError, challengeName := s.ready, s.startupError, s.challengeName
	s.mu.RUnlock()
	if startupError != "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": startupError, "challengeName": challengeName})
		return
	}
	if !ready {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "loading", "challengeName": challengeName})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := s.store.DB().PingContext(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "challengeName": challengeName})
}

func (s *Server) whenReady(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		ready, startupError := s.ready, s.startupError
		s.mu.RUnlock()
		if !ready {
			message := "deployment is still ingesting"
			if startupError != "" {
				message = startupError
			}
			writeError(w, http.StatusServiceUnavailable, message, nil)
			return
		}
		next(w, r)
	}
}

func (s *Server) schema(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	tableCatalog := s.tableCatalog
	s.mu.RUnlock()
	datasets, err := s.store.ListDatasets(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list tables", nil)
		return
	}
	challengeName, err := s.store.ChallengeName(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load workspace metadata", nil)
		return
	}
	sort.Slice(datasets, func(i, j int) bool {
		if datasets[i].Table == datasets[j].Table {
			return datasets[i].Name < datasets[j].Name
		}
		return datasets[i].Table < datasets[j].Table
	})
	var totalEvents int64
	tables := make([]map[string]any, 0, len(tableCatalog)+1)
	for _, dataset := range datasets {
		totalEvents += dataset.EventCount
	}
	tables = append(tables, map[string]any{
		"name": "Events", "description": "All datasets", "eventCount": totalEvents, "columns": schemaColumns(tableCatalog.Columns("Events")),
	})
	for index := 0; index < len(datasets); {
		dataset := datasets[index]
		if dataset.Table == "" {
			index++
			continue
		}
		eventCount := dataset.EventCount
		datasetNames := []string{dataset.Name}
		index++
		for index < len(datasets) && datasets[index].Table == dataset.Table {
			eventCount += datasets[index].EventCount
			datasetNames = append(datasetNames, datasets[index].Name)
			index++
		}
		tables = append(tables, map[string]any{
			"name": dataset.Table, "description": strings.Join(datasetNames, ", "), "eventCount": eventCount, "columns": schemaColumns(tableCatalog.Columns(dataset.Table)),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challengeName": challengeName,
		"tables":        tables,
		"statements":    []string{"let"},
		"operators":     []string{"where", "filter", "search", "project", "project-away", "project-keep", "project-rename", "project-reorder", "extend", "summarize", "distinct", "order by", "sort by", "top", "take", "limit", "sample", "sample-distinct", "count", "serialize", "as", "mv-expand", "mv-apply", "union", "join", "lookup"},
		"functions":     []string{"now", "ago", "datetime", "tostring", "toint", "tolong", "toreal", "todouble", "todatetime", "tolower", "toupper", "isnull", "isnotnull", "isempty", "isnotempty", "parse_json", "array_length", "bag_keys", "bag_has_key", "set_has_element", "base64_decode_tostring", "url_decode", "ipv4_is_private", "ipv4_is_in_range", "iff", "case", "coalesce", "strlen", "substring", "strcat", "split", "extract", "trim", "replace_string", "count", "countif", "sumif", "sum", "min", "max", "avg", "make_set", "make_list", "take_any"},
	})
}

func schemaColumns(fields []kql.Field) []map[string]string {
	columns := make([]map[string]string, len(fields))
	for index, field := range fields {
		columns[index] = map[string]string{"name": field.Name, "type": field.Type}
	}
	return columns
}

type queryRequest struct {
	Query string `json:"query"`
}

type answerRequest struct {
	Answer string `json:"answer"`
}

func (s *Server) questions(w http.ResponseWriter, r *http.Request) {
	state, err := s.store.ChallengeState(r.Context())
	if err != nil {
		s.logger.Error("Could not list investigation questions", "error", err)
		writeError(w, http.StatusInternalServerError, "could not load investigation questions", nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) submitAnswer(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json", nil)
		return
	}
	var request answerRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "answer is required", nil)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request must contain one JSON object", nil)
		return
	}
	request.Answer = strings.TrimSpace(request.Answer)
	if request.Answer == "" || len(request.Answer) > 512 {
		writeError(w, http.StatusBadRequest, "answer must contain 1 to 512 characters", nil)
		return
	}
	result, err := s.store.SubmitAnswer(r.Context(), r.PathValue("id"), request.Answer, time.Now().UTC())
	if errors.Is(err, database.ErrQuestionNotFound) {
		writeError(w, http.StatusNotFound, "question not found", nil)
		return
	}
	if err != nil {
		s.logger.Error("Could not submit investigation answer", "question", r.PathValue("id"), "error", err)
		writeError(w, http.StatusInternalServerError, "could not submit answer", nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if result.RetryAfter > 0 {
		retryMilliseconds := int64((result.RetryAfter + time.Millisecond - 1) / time.Millisecond)
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": "wait before trying again", "retryAfterMs": retryMilliseconds,
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) query(w http.ResponseWriter, r *http.Request) {
	compiled, ok := s.compileQuery(w, r, false)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	started := time.Now()
	rows, err := s.store.DB().QueryContext(ctx, compiled.SQL, compiled.Args...)
	if err != nil {
		s.logger.Error("Query execution failed", "error", err)
		writeQueryError(w, invalidSQLExpressionError())
		return
	}
	defer rows.Close()

	columns, results, err := scanRows(rows, compiled.DynamicColumns, compiled.BooleanColumns)
	if err != nil {
		s.logger.Error("Could not read query result", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read query result", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"columns":    columns,
		"rows":       results,
		"rowCount":   len(results),
		"durationMs": time.Since(started).Milliseconds(),
	})
}

func (s *Server) validateQuery(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.compileQuery(w, r, true); !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) compileQuery(w http.ResponseWriter, r *http.Request, prepare bool) (kql.CompiledQuery, bool) {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json", nil)
		return kql.CompiledQuery{}, false
	}
	var request queryRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || strings.TrimSpace(request.Query) == "" {
		writeError(w, http.StatusBadRequest, "query is required", nil)
		return kql.CompiledQuery{}, false
	}
	if len(request.Query) > 32<<10 {
		writeError(w, http.StatusBadRequest, "query cannot exceed 32 KiB", nil)
		return kql.CompiledQuery{}, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request must contain one JSON object", nil)
		return kql.CompiledQuery{}, false
	}

	s.mu.RLock()
	tableCatalog := s.tableCatalog
	s.mu.RUnlock()
	compiled, err := kql.Compile(request.Query, time.Now(), kql.CompileConfig{
		Tables:        tableCatalog,
		FullTextIndex: s.store.FullTextIndexEnabled(),
	})
	if err != nil {
		writeQueryError(w, err)
		return kql.CompiledQuery{}, false
	}
	if !prepare {
		return compiled, true
	}
	statement, err := s.prepareQuery(r.Context(), compiled.SQL)
	if err != nil {
		writeQueryError(w, invalidSQLExpressionError())
		return kql.CompiledQuery{}, false
	}
	statement.Close()
	return compiled, true
}

func requireSameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Striem-Request") != "1" {
			writeError(w, http.StatusForbidden, "request could not be verified", nil)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
			writeError(w, http.StatusForbidden, "cross-origin requests are not allowed", nil)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Host, r.Host) {
				writeError(w, http.StatusForbidden, "cross-origin requests are not allowed", nil)
				return
			}
		}
		next(w, r)
	}
}

func scanRows(rows *sql.Rows, dynamicColumns, booleanColumns map[string]struct{}) ([]string, []map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	results := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, nil, err
		}
		row := make(map[string]any, len(columns))
		for index, column := range columns {
			value := values[index]
			if _, boolean := booleanColumns[column]; boolean {
				switch current := value.(type) {
				case int64:
					value = current != 0
				case float64:
					value = current != 0
				}
			}
			if bytes, ok := value.([]byte); ok {
				value = string(bytes)
			}
			if text, ok := value.(string); ok {
				if _, dynamic := dynamicColumns[column]; !dynamic {
					row[column] = value
					continue
				}
				trimmed := strings.TrimSpace(text)
				raw := json.RawMessage(trimmed)
				if json.Valid(raw) {
					value = raw
				}
			}
			row[column] = value
		}
		results = append(results, row)
	}
	return columns, results, rows.Err()
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.logger.Enabled(r.Context(), slog.LevelDebug) {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		next.ServeHTTP(w, r)
		s.logger.DebugContext(r.Context(), r.Method+" "+r.URL.Path, "duration", time.Since(started))
	})
}

func writeQueryError(w http.ResponseWriter, err error) {
	var queryError *kql.Error
	if errors.As(err, &queryError) {
		writeError(w, http.StatusBadRequest, queryError.Message, map[string]int{"line": queryError.Line, "column": queryError.Column})
		return
	}
	writeError(w, http.StatusBadRequest, "query could not be executed", nil)
}

func invalidSQLExpressionError() *kql.Error {
	return &kql.Error{Message: "query contains an invalid column or expression", Line: 1, Column: 1}
}

func writeError(w http.ResponseWriter, status int, message string, position map[string]int) {
	response := map[string]any{"error": message}
	if position != nil {
		response["position"] = position
	}
	writeJSON(w, status, response)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode response", "error", err)
	}
}
