package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIKey(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	tests := []struct {
		name    string
		key     string
		headers map[string]string
		want    int
	}{
		{"disabled when no key configured", "", nil, http.StatusNoContent},
		{"missing key", "s3cret", nil, http.StatusUnauthorized},
		{"wrong key", "s3cret", map[string]string{"X-API-Key": "nope"}, http.StatusUnauthorized},
		{"x-api-key header", "s3cret", map[string]string{"X-API-Key": "s3cret"}, http.StatusNoContent},
		{"bearer token", "s3cret", map[string]string{"Authorization": "Bearer s3cret"}, http.StatusNoContent},
		{"basic auth is not accepted", "s3cret", map[string]string{"Authorization": "Basic s3cret"}, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			APIKey(tc.key)(ok).ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("got %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
