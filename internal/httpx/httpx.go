// Package httpx holds HTTP helpers shared by all feature packages.
package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
)

// PathID parses the named path parameter as a positive integer ID.
func PathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid %s %q", name, r.PathValue(name))
	}
	return id, nil
}

// WriteJSON writes body as JSON with the given status code.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("write response", "error", err)
	}
}

// WriteError writes {"error": message} with the given status code.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, errorBody{Error: message})
}

// InternalError logs err and sends a generic 500, so internal details
// never reach the client.
func InternalError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "request_id", RequestID(r.Context()), "method", r.Method, "path", r.URL.Path, "error", err)
	WriteError(w, http.StatusInternalServerError, "internal server error")
}

// Health returns a handler that reports 200 when check succeeds and 503
// otherwise. Load balancers use it to stop routing to unhealthy instances.
func Health(check func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := check(r.Context()); err != nil {
			slog.Warn("health check failed", "error", err)
			WriteError(w, http.StatusServiceUnavailable, "unhealthy")
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

type errorBody struct {
	Error string `json:"error"`
}
