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
	"strings"
	"time"

	"github.com/oka/striem/internal/database"
	"github.com/oka/striem/internal/kql"
	webassets "github.com/oka/striem/web"
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
	mux.HandleFunc("POST /api/query", s.query)

	static, err := fs.Sub(webassets.Files, "dist")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServerFS(static))
	return s.logRequests(mux)
}

func (s *Server) fields(w http.ResponseWriter, r *http.Request) {
	discovered, err := s.store.ListFields(r.Context())
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
	writeJSON(w, http.StatusOK, map[string]any{"common": common, "discovered": discovered})
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

func (s *Server) schema(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"tables": []map[string]any{{
			"name": "Events",
			"columns": []map[string]string{
				{"name": "TimeGenerated", "type": "datetime"},
				{"name": "Source", "type": "string"},
				{"name": "EventType", "type": "string"},
				{"name": "Host", "type": "string"},
				{"name": "User", "type": "string"},
				{"name": "Message", "type": "string"},
				{"name": "RawData", "type": "dynamic"},
			},
		}},
		"statements": []string{"let"},
		"operators":  []string{"where", "project", "extend", "summarize", "distinct", "order by", "sort by", "top", "take", "limit", "count"},
	})
}

type queryRequest struct {
	Query string `json:"query"`
}

func (s *Server) query(w http.ResponseWriter, r *http.Request) {
	var request queryRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || strings.TrimSpace(request.Query) == "" {
		writeError(w, http.StatusBadRequest, "query is required", nil)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request must contain one JSON object", nil)
		return
	}

	parsed, err := kql.Parse(request.Query)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	compiled, err := kql.Compile(parsed, time.Now())
	if err != nil {
		writeQueryError(w, err)
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

	results, err := scanRows(rows)
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

func scanRows(rows *sql.Rows) ([]map[string]any, error) {
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
			if column == "RawData" {
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
	writeError(w, http.StatusBadRequest, err.Error(), nil)
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
