// Package httpx holds HTTP helpers shared by all feature packages.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
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

// maxBodyBytes caps request bodies. API requests are a few fields; anything
// bigger is a mistake or an attack.
const maxBodyBytes = 1 << 16

// DecodeJSON decodes the request body into v. It rejects unknown fields, a
// body over maxBodyBytes, and anything after the JSON value, so client bugs
// surface as 400s instead of being silently ignored.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	if dec.More() {
		return errors.New("decode request body: unexpected data after the JSON value")
	}
	return nil
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
