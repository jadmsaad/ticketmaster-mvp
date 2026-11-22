// Package httpxtest holds helpers for testing HTTP handlers.
package httpxtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// Serve registers routes on a fresh mux and sends one request through it, so
// tests cover the route pattern and path parameters, not just the handler.
func Serve(t *testing.T, register func(*http.ServeMux), method, path string) *httptest.ResponseRecorder {
	t.Helper()
	return Do(t, register, httptest.NewRequestWithContext(t.Context(), method, path, nil))
}

// Do is Serve for a request the test builds itself, with a body or headers.
func Do(t *testing.T, register func(*http.ServeMux), req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// AssertJSON fails the test unless got and want are the same JSON value.
// Key order and whitespace are ignored, so want can be pretty-printed.
func AssertJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("expected body in test is not JSON: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("body mismatch\n got: %s\nwant: %s", got, want)
	}
}
