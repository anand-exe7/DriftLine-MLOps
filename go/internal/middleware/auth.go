package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"driftline/go/pkg/response"
)

// APIKey requires every request to carry the shared key, either as
// "Authorization: Bearer <key>" or "X-API-Key: <key>". An empty key disables
// the check (local development); main.go logs a warning when that happens.
//
// The comparison is constant-time so response timing doesn't leak how many
// leading characters of a guess were right.
func APIKey(key string) func(http.Handler) http.Handler {
	want := []byte(key)
	return func(next http.Handler) http.Handler {
		if key == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("X-API-Key")
			if got == "" {
				if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
					got = strings.TrimPrefix(auth, "Bearer ")
				}
			}
			if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="driftline"`)
				response.Error(w, http.StatusUnauthorized, "missing or invalid API key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
