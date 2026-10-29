package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

const testAccessKey = "team-7_k3y.0123456789"

func accessServer(t *testing.T) *httptest.Server {
	t.Helper()
	apiServer := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := apiServer.SetAccessKey(testAccessKey); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)
	return server
}

func TestAccessKeyGuardsEveryRouteButHealthChecks(t *testing.T) {
	server := accessServer(t)
	for _, path := range []string{"/", "/kusto.html", "/api/questions", "/api/schema", "/api/kusto/capabilities", "/?key=wrong-key-0123456789"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without the key = %d, want 401", path, response.StatusCode)
		}
	}
	response := queryAPI(t, server.URL, "print 1")
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Errorf("query without the key = %d, want 401", response.StatusCode)
	}
	for _, path := range []string{"/api/health", "/api/ready"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Errorf("%s = %d, want 200 for health checks", path, response.StatusCode)
		}
	}
}

func TestAccessLinkSetsCookieAndDropsTheKey(t *testing.T) {
	server := accessServer(t)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	response, err := client.Get(server.URL + "/?q=Events&key=" + testAccessKey)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "<html") {
		t.Fatalf("access link = %d", response.StatusCode)
	}
	if final := response.Request.URL; final.Query().Has("key") || final.Query().Get("q") != "Events" {
		t.Errorf("redirected to %s, want the key removed and other parameters kept", final)
	}
	cookies := jar.Cookies(response.Request.URL)
	if len(cookies) != 1 || !strings.HasPrefix(cookies[0].Name, "striem_access_") {
		t.Fatalf("cookies = %v", cookies)
	}
	questions, err := client.Get(server.URL + "/api/questions")
	if err != nil {
		t.Fatal(err)
	}
	questions.Body.Close()
	if questions.StatusCode != http.StatusOK {
		t.Errorf("questions with the cookie = %d", questions.StatusCode)
	}
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/questions", nil)
	request.Header.Set("Authorization", "Bearer "+testAccessKey)
	bearer, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	bearer.Body.Close()
	if bearer.StatusCode != http.StatusOK {
		t.Errorf("questions with a bearer key = %d", bearer.StatusCode)
	}
}

func TestAccessKeyValidation(t *testing.T) {
	server := &Server{}
	for _, key := range []string{"short", "has spaces in the key", "semi;colon-0123456789", "ünïcode-0123456789"} {
		if err := server.SetAccessKey(key); err == nil {
			t.Errorf("SetAccessKey(%q) was accepted", key)
		}
	}
	if err := server.SetAccessKey(""); err != nil || server.accessKeyHash != nil {
		t.Errorf("empty key = %v, %v; want the check disabled", server.accessKeyHash, err)
	}
}
