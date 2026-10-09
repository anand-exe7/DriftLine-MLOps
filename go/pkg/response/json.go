// Package response holds the JSON envelope every HTTP handler uses, so the
// dashboard can rely on one shape for both success and error bodies.
package response

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type errorBody struct {
	Error string `json:"error"`
}

// JSON writes v as the response body with the given status code.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers are already sent; all we can do is log.
		slog.Error("response: encode failed", "err", err)
	}
}

// Error writes {"error": msg}.
func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, errorBody{Error: msg})
}

// Decode reads a JSON request body into dst, rejecting unknown fields so
// typos in client payloads fail loudly instead of being silently ignored.
func Decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
