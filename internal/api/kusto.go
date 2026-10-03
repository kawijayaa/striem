package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kawijayaa/ksql/kusto"
)

// NativeKustoConfig is an opt-in service connection. All native requests use the
// configured database and service principal. Callers cannot select an endpoint,
// credential or database through the HTTP API. Service authorization still
// governs KQL that refers to another database, cluster or external service.
type NativeKustoConfig struct {
	Client          *kusto.Client
	Database        string
	AllowManagement bool
}

func (s *Server) SetNativeKusto(config NativeKustoConfig) error {
	if config.Client != nil && (strings.TrimSpace(config.Database) == "" || len(config.Database) > 4096 || !utf8.ValidString(config.Database)) {
		return errors.New("native Kusto requires a valid database")
	}
	s.mu.Lock()
	s.nativeKusto = config
	s.mu.Unlock()
	return nil
}

func (s *Server) nativeQuery(w http.ResponseWriter, r *http.Request) {
	s.executeNativeKusto(w, r, false)
}
func (s *Server) nativeManagement(w http.ResponseWriter, r *http.Request) {
	s.executeNativeKusto(w, r, true)
}

func (s *Server) executeNativeKusto(w http.ResponseWriter, r *http.Request, management bool) {
	w.Header().Set("Cache-Control", "no-store")
	s.mu.RLock()
	config := s.nativeKusto
	s.mu.RUnlock()
	if config.Client == nil {
		writeError(w, http.StatusNotFound, "native Kusto is not configured", nil)
		return
	}
	if management && !config.AllowManagement {
		writeError(w, http.StatusForbidden, "native management is disabled", nil)
		return
	}
	if contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || contentType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json", nil)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil || !utf8.Valid(body) {
		writeError(w, http.StatusBadRequest, "invalid native request body", nil)
		return
	}
	var request struct {
		Query       string                     `json:"query"`
		Parameters  map[string]json.RawMessage `json:"parameters"`
		Progressive json.RawMessage            `json:"progressive"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || strings.TrimSpace(request.Query) == "" || len(request.Query) > 32<<10 {
		writeError(w, http.StatusBadRequest, "query is required and cannot exceed 32 KiB", nil)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request must contain one JSON object", nil)
		return
	}
	progressive := false
	if len(request.Progressive) > 0 && (bytes.Equal(bytes.TrimSpace(request.Progressive), []byte("null")) || json.Unmarshal(request.Progressive, &progressive) != nil) {
		writeError(w, http.StatusBadRequest, "progressive must be a boolean", nil)
		return
	}
	parameters := make(map[string]string, len(request.Parameters))
	for name, raw := range request.Parameters {
		var value string
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil {
			writeError(w, http.StatusBadRequest, "query parameter values must be strings", nil)
			return
		}
		parameters[name] = value
	}
	if management && progressive {
		writeError(w, http.StatusBadRequest, "progressive results require the native query endpoint", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	serviceRequest := kusto.Request{Database: config.Database, Text: request.Query, Parameters: parameters, Progressive: progressive}
	var result *kusto.Result
	if management {
		result, err = config.Client.Management(ctx, serviceRequest)
	} else {
		// The service enforces this property and KQL set statements cannot
		// override it. Browser callers cannot supply client request options.
		serviceRequest.Options = map[string]any{"request_readonly": true}
		result, err = config.Client.Query(ctx, serviceRequest)
	}
	if err != nil {
		status := http.StatusBadGateway
		message := "native Kusto request failed"
		if errors.Is(err, context.DeadlineExceeded) {
			status, message = http.StatusGatewayTimeout, "native Kusto request timed out"
		}
		if errors.Is(err, kusto.ErrLimit) {
			message = "native Kusto result exceeded resource limits"
		}
		var serviceError *kusto.ServiceError
		if errors.As(err, &serviceError) {
			if serviceError.StatusCode == http.StatusBadRequest {
				status = http.StatusBadRequest
			}
			if serviceError.StatusCode == http.StatusTooManyRequests {
				status = http.StatusTooManyRequests
			}
			writeJSON(w, status, map[string]any{"error": message, "requestId": serviceError.RequestID, "activityId": serviceError.ActivityID})
		} else {
			writeError(w, status, message, nil)
		}
		return
	}
	tables := make([]nativeTable, 0, len(result.Tables))
	for _, table := range result.Tables {
		columns := make([]nativeColumn, 0, len(table.Columns))
		for _, column := range table.Columns {
			columns = append(columns, nativeColumn{column.Name, column.Type})
		}
		rows := table.Rows
		if rows == nil {
			rows = [][]json.RawMessage{}
		}
		tables = append(tables, nativeTable{ID: table.ID, Kind: table.Kind, Name: table.Name, Columns: columns, Rows: rows, Progress: table.Progress})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tables": tables, "requestId": result.RequestID, "activityId": result.ActivityID, "progressive": result.Progressive})
}

type nativeColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type nativeTable struct {
	ID       int                 `json:"id"`
	Kind     string              `json:"kind"`
	Name     string              `json:"name"`
	Columns  []nativeColumn      `json:"columns"`
	Rows     [][]json.RawMessage `json:"rows"`
	Progress *float64            `json:"progress,omitempty"`
}
