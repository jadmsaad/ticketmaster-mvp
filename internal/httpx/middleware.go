package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name: "http_request_duration_seconds",
	Help: "HTTP request latency, by route pattern and status code.",
	// Finer buckets at the low end, where database-bound requests land.
	Buckets: []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
}, []string{"method", "route", "status"})

var quietRoutes = map[string]bool{"GET /healthz": true, "GET /metrics": true}

type requestIDKey struct{}

// RequestID returns the request's ID, or "" outside a request.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// Observe wraps a handler to give every request an ID, log it, and record its
// latency. Wrap the ServeMux with it, so the route pattern is known.
//
// It reuses an incoming X-Request-ID (e.g. set by the load balancer), so one
// ID follows a request across services.
func Observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		elapsed := time.Since(start)

		// Label by pattern ("GET /events/{id}"), never by raw path, so metrics
		// don't grow one series per event ID. The mux sets r.Pattern on match.
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		requestDuration.WithLabelValues(r.Method, route, strconv.Itoa(rec.status)).Observe(elapsed.Seconds())

		// Health checks and metrics scrapes run every few seconds; logging
		// them would bury real traffic.
		if quietRoutes[route] {
			return
		}
		slog.Info("request",
			"request_id", id,
			"method", r.Method,
			"path", r.URL.Path,
			"route", route,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", float64(elapsed.Microseconds())/1000,
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
