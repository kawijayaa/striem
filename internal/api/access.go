package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

// minAccessKeyLength keeps access keys out of reach of guessing. Keys use
// characters that links and cookies carry unchanged.
const (
	minAccessKeyLength  = 16
	accessKeyCharacters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~"
)

// SetAccessKey requires every request, except health and readiness checks,
// to present key: once through a ?key= link, which stores it in a cookie, or
// as a bearer token. Striem has no accounts, so a key lets a CTF give each
// team a deployment only that team can open. An empty key disables the check.
func (s *Server) SetAccessKey(key string) error {
	if key != "" && (len(key) < minAccessKeyLength || strings.TrimLeft(key, accessKeyCharacters) != "") {
		return errors.New("access key must have at least 16 letters, digits, '-', '_', '.' or '~'")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == "" {
		s.accessKeyHash = nil
		return nil
	}
	hash := sha256.Sum256([]byte(key))
	s.accessKeyHash = hash[:]
	return nil
}

// accessCookieName differs per key, so deployments sharing a host name on
// different ports keep their own cookies.
func accessCookieName(keyHash []byte) string {
	return "striem_access_" + hex.EncodeToString(keyHash[:4])
}

func matchesAccessKey(keyHash []byte, candidate string) bool {
	hash := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(hash[:], keyHash) == 1
}

func (s *Server) requireAccessKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		keyHash := s.accessKeyHash
		s.mu.RUnlock()
		if keyHash == nil || r.URL.Path == "/api/health" || r.URL.Path == "/api/ready" {
			next.ServeHTTP(w, r)
			return
		}
		cookieName := accessCookieName(keyHash)
		query := r.URL.Query()
		if query.Has("key") {
			if !matchesAccessKey(keyHash, query.Get("key")) {
				denyAccess(w, r)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: cookieName, Value: query.Get("key"), Path: "/", MaxAge: 30 * 24 * 60 * 60,
				HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
			})
			// Drop the key from the address bar and history.
			query.Del("key")
			location := *r.URL
			location.RawQuery = query.Encode()
			http.Redirect(w, r, location.RequestURI(), http.StatusSeeOther)
			return
		}
		if cookie, err := r.Cookie(cookieName); err == nil && matchesAccessKey(keyHash, cookie.Value) {
			next.ServeHTTP(w, r)
			return
		}
		if token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); found && matchesAccessKey(keyHash, token) {
			next.ServeHTTP(w, r)
			return
		}
		denyAccess(w, r)
	})
}

func denyAccess(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusUnauthorized, "this deployment requires its access link", nil)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte("This Striem deployment requires its access link. Open the link your organizers provided.\n"))
}
