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
	"time"

	"github.com/kawijayaa/striem/internal/database"
	"github.com/kawijayaa/striem/internal/kql"
	webassets "github.com/kawijayaa/striem/web"
)

type Server struct {
	store  *database.Store
	logger *slog.Logger
}

func New(store *database.Store, logger *slog.Logger) *Server {
	return &Server{store: store, logger: logger}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/ready", s.health)
	mux.HandleFunc("GET /api/schema", s.schema)
	mux.HandleFunc("GET /api/fields", s.fields)
	mux.HandleFunc("GET /api/questions", s.questions)
	mux.HandleFunc("POST /api/questions/{id}/answer", requireSameOrigin(s.submitAnswer))
	mux.HandleFunc("POST /api/query", requireSameOrigin(s.query))
	mux.HandleFunc("POST /api/query/validate", requireSameOrigin(s.validateQuery))

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
		{Path: "EventType", Type: "string"},
		{Path: "Host", Type: "string"},
		{Path: "User", Type: "string"},
		{Path: "Message", Type: "string"},
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

func (s *Server) schema(w http.ResponseWriter, r *http.Request) {
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
	columns := []map[string]string{
		{"name": "TimeGenerated", "type": "datetime"},
		{"name": "Source", "type": "string"},
		{"name": "EventType", "type": "string"},
		{"name": "Host", "type": "string"},
		{"name": "User", "type": "string"},
		{"name": "Message", "type": "string"},
		{"name": "RawData", "type": "dynamic"},
	}
	sort.Slice(datasets, func(i, j int) bool { return datasets[i].Table < datasets[j].Table })
	var totalEvents int64
	tables := make([]map[string]any, 0, len(datasets)+1)
	for _, dataset := range datasets {
		totalEvents += dataset.EventCount
	}
	tables = append(tables, map[string]any{
		"name": "Events", "description": "All datasets", "eventCount": totalEvents, "columns": columns,
	})
	for _, dataset := range datasets {
		if dataset.Table == "" {
			continue
		}
		tables = append(tables, map[string]any{
			"name": dataset.Table, "description": dataset.Name, "eventCount": dataset.EventCount, "columns": columns,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challengeName": challengeName,
		"tables":        tables,
		"statements":    []string{"let"},
		"operators":     []string{"where", "search", "project", "extend", "summarize", "distinct", "order by", "sort by", "top", "take", "limit", "count", "mv-expand", "mv-apply", "union", "join"},
	})
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
		s.logger.Error("list investigation questions", "error", err)
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
		s.logger.Error("submit investigation answer", "question", r.PathValue("id"), "error", err)
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
	compiled, ok := s.compileQuery(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	started := time.Now()
	rows, err := s.store.DB().QueryContext(ctx, compiled.SQL, compiled.Args...)
	if err != nil {
		s.logger.Error("query execution failed", "error", err)
		writeError(w, http.StatusBadRequest, "query could not be executed", nil)
		return
	}
	defer rows.Close()

	results, err := scanRows(rows, compiled.DynamicColumns)
	if err != nil {
		s.logger.Error("query result failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read query result", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"columns":    compiled.Columns,
		"rows":       results,
		"rowCount":   len(results),
		"durationMs": time.Since(started).Milliseconds(),
	})
}

func (s *Server) validateQuery(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.compileQuery(w, r); !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) compileQuery(w http.ResponseWriter, r *http.Request) (kql.CompiledQuery, bool) {
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

	parsed, err := kql.Parse(request.Query)
	if err != nil {
		writeQueryError(w, err)
		return kql.CompiledQuery{}, false
	}
	datasets, err := s.store.ListDatasets(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not resolve query tables", nil)
		return kql.CompiledQuery{}, false
	}
	catalog := make(kql.TableCatalog, len(datasets))
	for _, dataset := range datasets {
		if dataset.Table != "" {
			catalog[dataset.Table] = dataset.ID
		}
	}
	compiled, err := kql.Compile(parsed, time.Now(), catalog)
	if err != nil {
		writeQueryError(w, err)
		return kql.CompiledQuery{}, false
	}
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

func scanRows(rows *sql.Rows, dynamicColumns map[string]struct{}) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(columns))
		for index, column := range columns {
			value := values[index]
			if bytes, ok := value.([]byte); ok {
				value = string(bytes)
			}
			_, dynamic := dynamicColumns[column]
			if dynamic || strings.TrimRight(column, "0123456789") == "RawData" {
				if text, ok := value.(string); ok {
					var raw any
					if json.Unmarshal([]byte(text), &raw) == nil {
						value = raw
					}
				}
			}
			row[column] = value
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
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
