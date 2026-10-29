package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// maxClientQueries is how many queries one client may run at once, so a
// client looping slow queries cannot hold every query connection and lock
// everyone else out of the deployment. Further queries from the client wait
// for one of its slots, since teammates often share one address, but at most
// maxClientWaiting of them wait, each for at most maxClientWait.
const (
	maxClientQueries = 2
	maxClientWaiting = 8
	maxClientWait    = 5 * time.Second
)

type clientSlots struct {
	running chan struct{}
	users   int
}

type clientLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientSlots
}

// acquire waits for one of the client's query slots. It reports false when
// too many of the client's queries already wait, or no slot frees in time.
func (l *clientLimiter) acquire(ctx context.Context, client string) bool {
	l.mu.Lock()
	if l.clients == nil {
		l.clients = make(map[string]*clientSlots)
	}
	slots := l.clients[client]
	if slots == nil {
		slots = &clientSlots{running: make(chan struct{}, maxClientQueries)}
		l.clients[client] = slots
	}
	if slots.users >= maxClientQueries+maxClientWaiting {
		l.mu.Unlock()
		return false
	}
	slots.users++
	l.mu.Unlock()
	timer := time.NewTimer(maxClientWait)
	defer timer.Stop()
	select {
	case slots.running <- struct{}{}:
		return true
	case <-ctx.Done():
	case <-timer.C:
	}
	l.leave(client, slots)
	return false
}

func (l *clientLimiter) release(client string) {
	l.mu.Lock()
	slots := l.clients[client]
	l.mu.Unlock()
	<-slots.running
	l.leave(client, slots)
}

func (l *clientLimiter) leave(client string, slots *clientSlots) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if slots.users--; slots.users == 0 {
		delete(l.clients, client)
	}
}

// SetClientIPHeader names a header, such as CF-Connecting-IP, that a trusted
// reverse proxy sets to the client address. Leave it empty unless every
// request passes through that proxy; clients can forge the header otherwise.
func (s *Server) SetClientIPHeader(name string) {
	s.mu.Lock()
	s.clientIPHeader = http.CanonicalHeaderKey(strings.TrimSpace(name))
	s.mu.Unlock()
}

// clientKey identifies the client that sent a request: the trusted proxy
// header when one is configured, and the connection's address otherwise.
// IPv6 addresses are grouped by /64, which one host can rotate within.
func (s *Server) clientKey(r *http.Request) string {
	s.mu.RLock()
	header := s.clientIPHeader
	s.mu.RUnlock()
	address := ""
	if header != "" {
		// A proxy appends to X-Forwarded-For, so the last entry is the one
		// the trusted proxy wrote.
		values := strings.Split(r.Header.Get(header), ",")
		address = strings.TrimSpace(values[len(values)-1])
	}
	if address == "" {
		address = r.RemoteAddr
		if host, _, err := net.SplitHostPort(address); err == nil {
			address = host
		}
	}
	parsed, err := netip.ParseAddr(address)
	if err != nil {
		return address
	}
	parsed = parsed.Unmap()
	if parsed.Is6() {
		if prefix, err := parsed.Prefix(64); err == nil {
			return prefix.String()
		}
	}
	return parsed.String()
}

// limitClientQueries holds a query until its client has a free slot.
func (s *Server) limitClientQueries(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client := s.clientKey(r)
		if !s.queryLimiter.acquire(r.Context(), client) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "too many queries are running from this client; wait for one to finish", nil)
			return
		}
		defer s.queryLimiter.release(client)
		next(w, r)
	}
}

// logLimiter bounds how often clients can make the server log an error.
type logLimiter struct {
	mu     sync.Mutex
	window time.Time
	count  int
}

// maxErrorLogsPerMinute is how many query errors are logged each minute.
const maxErrorLogsPerMinute = 30

func (l *logLimiter) allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.window) >= time.Minute {
		l.window, l.count = now, 0
	}
	l.count++
	return l.count <= maxErrorLogsPerMinute
}
