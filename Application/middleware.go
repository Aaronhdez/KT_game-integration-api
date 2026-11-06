package application

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder is a wrapper around http.ResponseWriter that captures the HTTP status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// RequestLogger is a middleware that logs HTTP requests including method, path, status, and duration.
func RequestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r)
			logger.Info("request completed",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", time.Since(start),
			)
		})
}
