package middleware

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"driftline/go/internal/metrics"
)

// Logger emits one structured log line per request after it completes, and
// records Prometheus request count + latency labelled by route pattern.
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor) // captures status + bytes
			next.ServeHTTP(ww, r)
			elapsed := time.Since(start)

			// Label by pattern ("/api/v1/models/{name}"), not raw path, so
			// metric cardinality stays bounded no matter what URLs clients send.
			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			metrics.HTTPRequests.WithLabelValues(route, r.Method, strconv.Itoa(ww.Status())).Inc()
			metrics.HTTPDuration.WithLabelValues(route, r.Method).Observe(elapsed.Seconds())

			if route == "/metrics" || route == "/healthz" {
				return // scraped every few seconds; logging them is noise
			}
			level := slog.LevelInfo
			if ww.Status() >= 500 {
				level = slog.LevelError
			}
			log.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("duration", elapsed),
				slog.String("request_id", chimw.GetReqID(r.Context())),
			)
		})
	}
}
