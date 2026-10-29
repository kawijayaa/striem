package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kawijayaa/striem/internal/database"
)

func TestClientKeys(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/api/query", nil)
	request.RemoteAddr = "198.51.100.7:51000"
	request.Header.Set("CF-Connecting-IP", "203.0.113.9")
	request.Header.Set("X-Forwarded-For", "192.0.2.1, 203.0.113.10")
	if got := server.clientKey(request); got != "198.51.100.7" {
		t.Errorf("untrusted header key = %q, want the connection address", got)
	}
	server.SetClientIPHeader("cf-connecting-ip")
	if got := server.clientKey(request); got != "203.0.113.9" {
		t.Errorf("trusted header key = %q", got)
	}
	// The last X-Forwarded-For entry is the one the trusted proxy appended.
	server.SetClientIPHeader("X-Forwarded-For")
	if got := server.clientKey(request); got != "203.0.113.10" {
		t.Errorf("forwarded key = %q", got)
	}
	request.Header.Del("X-Forwarded-For")
	if got := server.clientKey(request); got != "198.51.100.7" {
		t.Errorf("missing header key = %q, want the connection address", got)
	}
	server.SetClientIPHeader("")
	for _, address := range []string{"[2001:db8:1:2:aaaa::1]:443", "[2001:db8:1:2:bbbb::9]:443"} {
		request.RemoteAddr = address
		if got := server.clientKey(request); got != "2001:db8:1:2::/64" {
			t.Errorf("IPv6 key for %s = %q, want its /64", address, got)
		}
	}
	request.RemoteAddr = "[::ffff:198.51.100.7]:1"
	if got := server.clientKey(request); got != "198.51.100.7" {
		t.Errorf("mapped IPv4 key = %q", got)
	}
}

func TestClientLimiterQueuesEachClient(t *testing.T) {
	var limiter clientLimiter
	ctx := t.Context()
	for range maxClientQueries {
		if !limiter.acquire(ctx, "a") {
			t.Fatal("slot refused below the limit")
		}
	}
	if !limiter.acquire(ctx, "b") {
		t.Fatal("another client was refused")
	}
	// A further query waits for one of its own client's slots.
	acquired := make(chan bool)
	go func() { acquired <- limiter.acquire(ctx, "a") }()
	select {
	case <-acquired:
		t.Fatal("slot granted above the limit")
	case <-time.After(50 * time.Millisecond):
	}
	limiter.release("a")
	if !<-acquired {
		t.Fatal("waiting query did not get the released slot")
	}
	// A query whose request ends stops waiting.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if limiter.acquire(canceled, "a") {
		t.Fatal("canceled query got a slot")
	}
	for range maxClientQueries {
		limiter.release("a")
	}
	limiter.release("b")
	if len(limiter.clients) != 0 {
		t.Errorf("idle clients remain tracked: %v", limiter.clients)
	}
}

// Beyond maxClientWaiting waiting queries, a client's queries are refused
// at once rather than piling up.
func TestClientLimiterRefusesExcessWaiters(t *testing.T) {
	var limiter clientLimiter
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for range maxClientQueries {
		limiter.acquire(ctx, "a")
	}
	waiting := make(chan bool, maxClientWaiting)
	for range maxClientWaiting {
		go func() { waiting <- limiter.acquire(ctx, "a") }()
	}
	deadline := time.Now().Add(time.Second)
	for {
		limiter.mu.Lock()
		users := limiter.clients["a"].users
		limiter.mu.Unlock()
		if users == maxClientQueries+maxClientWaiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("users = %d, want %d", users, maxClientQueries+maxClientWaiting)
		}
		time.Sleep(time.Millisecond)
	}
	started := time.Now()
	if limiter.acquire(t.Context(), "a") || time.Since(started) > 100*time.Millisecond {
		t.Fatal("an excess waiter was not refused at once")
	}
	cancel()
	for range maxClientWaiting {
		if <-waiting {
			t.Error("a canceled waiter got a slot")
		}
	}
}

// Pivot key discovery runs queries during compilation. They share the
// request's deadline, so chaining many discoveries cannot run for minutes.
func TestDiscoveryQueriesShareTheQueryDeadline(t *testing.T) {
	server := serveStore(t, testStore(t))
	part := `(range i from 1 to 600000 step 1 | extend v = tostring(i % 3) | evaluate pivot(v) | take 1)`
	parts := make([]string, 40)
	for index := range parts {
		parts[index] = part
	}
	query := "union " + strings.Join(parts, ", ")
	for _, path := range []string{"/api/query", "/api/query/validate"} {
		started := time.Now()
		response := queryAPIPath(t, server.URL+path, query)
		var body struct {
			Error string `json:"error"`
		}
		json.NewDecoder(response.Body).Decode(&body)
		response.Body.Close()
		elapsed := time.Since(started)
		if elapsed > queryTimeLimit+2*time.Second {
			t.Errorf("%s ran for %v", path, elapsed)
		}
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(body.Error, "time limit") {
			t.Errorf("%s = %d %q, want the time limit error", path, response.StatusCode, body.Error)
		}
	}
}

// A GROUP BY over many large distinct values builds an in-memory b-tree.
// The SQLite memory limit stops it with an explanation instead of letting
// it grow by gigabytes within the deadline.
func TestQueriesStopAtTheMemoryLimit(t *testing.T) {
	store := testStore(t)
	if err := store.LimitMemory(t.Context(), 64<<20); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.LimitMemory(context.Background(), database.DefaultMemoryLimit) })
	var limit int64
	if err := store.DB().QueryRow("PRAGMA hard_heap_limit").Scan(&limit); err != nil || limit != 64<<20 {
		t.Fatalf("hard_heap_limit = %d, %v", limit, err)
	}
	server := serveStore(t, store)
	response := queryAPI(t, server.URL, `range i from 1 to 100000000 step 1 | extend s = strcat(strrep("a", 4000), i) | summarize by s | count`)
	defer response.Body.Close()
	var body struct {
		Error string `json:"error"`
	}
	json.NewDecoder(response.Body).Decode(&body)
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(body.Error, "more memory than the server allows") {
		t.Errorf("status = %d %q, want the memory limit error", response.StatusCode, body.Error)
	}
	// Ordinary queries still run once the failed query releases its memory.
	if rows := queryRows(t, server.URL, `range i from 1 to 1000 step 1 | summarize by tostring(i) | count`); len(rows) != 1 {
		t.Errorf("rows after the memory limit = %v", rows)
	}
}

func TestLogLimiterBoundsErrorsPerMinute(t *testing.T) {
	var limiter logLimiter
	now := time.Unix(1000, 0)
	allowed := 0
	for range 3 * maxErrorLogsPerMinute {
		if limiter.allow(now) {
			allowed++
		}
	}
	if allowed != maxErrorLogsPerMinute {
		t.Errorf("allowed %d logs in a minute, want %d", allowed, maxErrorLogsPerMinute)
	}
	if !limiter.allow(now.Add(time.Minute)) {
		t.Error("the next minute was not allowed to log")
	}
}

// Failures a query causes itself are not server errors, so repeating them
// leaves nothing in the error log.
func TestQueryCausedFailuresAreNotLoggedAsErrors(t *testing.T) {
	var logs bytes.Buffer
	server := httptest.NewServer(New(testStore(t), slog.New(slog.NewTextHandler(&logs, nil))).Handler())
	t.Cleanup(server.Close)
	for range 5 {
		response := queryAPI(t, server.URL, `print assert(1 == 2, "nope")`)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", response.StatusCode)
		}
	}
	if logs.Len() != 0 {
		t.Errorf("query failures were logged:\n%s", logs.String())
	}
}
