package api

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kawijayaa/ksql/kusto"
)

func TestNativeKustoCapabilities(t *testing.T) {
	client, err := kusto.New(kusto.Config{Endpoint: "https://private.example.invalid", TokenProvider: func(context.Context) (string, error) { t.Fatal("capabilities accessed credentials"); return "", nil }})
	if err != nil {
		t.Fatal(err)
	}
	server := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tc := range []struct {
		config NativeKustoConfig
		want   string
	}{
		{NativeKustoConfig{}, `{"enabled":false,"management":false}`},
		{NativeKustoConfig{AllowManagement: true}, `{"enabled":false,"management":false}`},
		{NativeKustoConfig{Client: client, Database: "private database"}, `{"enabled":true,"management":false}`},
		{NativeKustoConfig{Client: client, Database: "private database", AllowManagement: true}, `{"enabled":true,"management":true}`},
	} {
		if err := server.SetNativeKusto(tc.config); err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/api/kusto/capabilities", nil))
		if recorder.Code != 200 || strings.TrimSpace(recorder.Body.String()) != tc.want {
			t.Fatalf("capabilities: %d %s", recorder.Code, recorder.Body.String())
		}
		if recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("capabilities cached")
		}
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/kusto.html", nil))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `id="native-form"`) {
		t.Fatalf("native page unavailable: %d", recorder.Code)
	}
}
