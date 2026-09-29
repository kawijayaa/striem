package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kawijayaa/striem/internal/database"
)

func TestAllDataRoutesFollowReadinessLifecycle(t *testing.T) {
	store := testStore(t)
	api := New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := api.Handler()
	routes := []struct{ method, path, body string }{
		{"GET", "/api/schema", ""}, {"GET", "/api/fields", ""}, {"GET", "/api/questions", ""},
		{"POST", "/api/query", `{"query":"Events | count"}`},
		{"POST", "/api/query/validate", `{"query":"Events | count"}`},
		{"POST", "/api/questions/missing/answer", `{"answer":"yes"}`},
	}
	for _, state := range []string{"loading", "error", "loading"} {
		if state == "error" {
			api.SetStartupError("failed fixture")
		} else {
			api.SetLoading()
		}
		for _, route := range routes {
			request := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Striem-Request", "1")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusServiceUnavailable || !json.Valid(response.Body.Bytes()) {
				t.Fatalf("%s in %s: %d %s", route.path, state, response.Code, response.Body.String())
			}
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/ready", nil))
		var result map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result["status"] != state || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("readiness: %#v", result)
		}
	}
	if err := api.RefreshCatalog(t.Context()); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/ready", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("refresh did not restore readiness: %s", response.Body.String())
	}
}

func TestCanceledQueriesReleaseDatabaseWait(t *testing.T) {
	store := testStore(t)
	api := New(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := api.Handler()
	store.DB().SetMaxOpenConns(1)
	for _, path := range []string{"/api/query", "/api/query/validate"} {
		t.Run(path, func(t *testing.T) {
			connection, err := store.DB().Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			request := httptest.NewRequest("POST", path, strings.NewReader(`{"query":"Events | count"}`)).WithContext(ctx)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Striem-Request", "1")
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { defer close(done); handler.ServeHTTP(response, request) }()
			cancel()
			select {
			case <-done:
				if response.Code == http.StatusOK || response.Code == http.StatusNoContent || !json.Valid(response.Body.Bytes()) {
					t.Fatalf("canceled query response: %d %s", response.Code, response.Body.String())
				}
			case <-time.After(time.Second):
				t.Fatal("query ignored request cancellation while waiting for a database connection")
			}
		})
	}
	if err := store.DB().PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestAnswerValidationDoesNotChangeProgress(t *testing.T) {
	store := testStore(t)
	if err := store.ConfigureChallenge(t.Context(), database.ChallengeDefinition{Flag: "secret", Questions: []database.QuestionDefinition{{ID: "one", Revision: 1, AcceptedAnswers: []string{"yes"}}}}); err != nil {
		t.Fatal(err)
	}
	handler := New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	for _, body := range []string{`{`, `null`, `{}`, `{"answer":5}`, `{"answer":"   "}`, `{"answer":"yes","extra":true}`, `{"answer":"yes"} {}`, string(mustJSON(t, map[string]string{"answer": strings.Repeat("x", 513)})), string(mustJSON(t, map[string]string{"answer": strings.Repeat("x", 5000)}))} {
		request := httptest.NewRequest("POST", "/api/questions/one/answer", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Striem-Request", "1")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || !json.Valid(response.Body.Bytes()) {
			t.Fatalf("invalid answer returned %d: %s", response.Code, response.Body.String())
		}
	}
	state, err := store.ChallengeState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if state.Questions[0].Attempts != 0 || state.Completed || state.Flag != "" {
		t.Fatalf("invalid request changed challenge: %#v", state)
	}
}

func TestAnswerSupportsUnicodeCharacterLimit(t *testing.T) {
	store := testStore(t)
	answer := strings.Repeat("界", 512)
	if err := store.ConfigureChallenge(t.Context(), database.ChallengeDefinition{Flag: "secret", Questions: []database.QuestionDefinition{{ID: "unicode", Revision: 1, AcceptedAnswers: []string{answer}}}}); err != nil {
		t.Fatal(err)
	}
	server := serveStore(t, store)
	result := submitQuestion(t, server.URL, "unicode", answer)
	if result.StatusCode != http.StatusOK || !result.Body.Correct {
		t.Fatalf("512 character Unicode answer rejected: %#v", result)
	}
}

func TestStaticRoutesAndUnsupportedMethods(t *testing.T) {
	_, server := testServer(t)
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/", http.StatusOK}, {"HEAD", "/", http.StatusOK},
		{"GET", "/missing.js", http.StatusNotFound}, {"GET", "/api/missing", http.StatusNotFound},
		{"GET", "/go.mod", http.StatusNotFound}, {"GET", "/../go.mod", http.StatusNotFound},
	} {
		request, err := http.NewRequest(test.method, server.URL+test.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != test.status {
			t.Fatalf("%s %s: %d", test.method, test.path, response.StatusCode)
		}
		if test.method == "HEAD" && len(body) != 0 {
			t.Fatal("HEAD returned a body")
		}
	}
	for _, path := range []string{"/api/query", "/api/query/validate", "/api/questions/id/answer", "/api/health", "/api/ready", "/api/schema", "/api/fields", "/api/questions"} {
		request, err := http.NewRequest("DELETE", server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode < 400 {
			t.Fatalf("unsupported DELETE %s succeeded: %d", path, response.StatusCode)
		}
	}
}
