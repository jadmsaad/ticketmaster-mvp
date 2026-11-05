package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/expfmt"
)

func TestObserve(t *testing.T) {
	requestDuration.Reset()
	var seenID string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /things/{id}", func(w http.ResponseWriter, r *http.Request) {
		seenID = RequestID(r.Context())
		WriteError(w, http.StatusNotFound, "nope")
	})
	handler := Observe(mux)

	// An incoming request ID is reused and returned.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/things/7", nil)
	req.Header.Set("X-Request-ID", "abc123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if seenID != "abc123" || rec.Header().Get("X-Request-ID") != "abc123" {
		t.Fatalf("request ID: handler saw %q, response header %q, want abc123", seenID, rec.Header().Get("X-Request-ID"))
	}

	// A request with no ID gets a generated one.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nowhere", nil))
	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("no request ID generated")
	}

	// Latency is labelled by route pattern, never the raw path, and
	// unrouted requests share one "unmatched" label.
	want := `
		http_request_duration_seconds_count{method="GET",route="GET /things/{id}",status="404"} 1
		http_request_duration_seconds_count{method="GET",route="unmatched",status="404"} 1
	`
	got, err := testutil.CollectAndFormat(requestDuration, expfmt.TypeTextPlain, "http_request_duration_seconds")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(want), "\n") {
		if !strings.Contains(string(got), strings.TrimSpace(line)) {
			t.Errorf("metrics missing %q\ngot:\n%s", strings.TrimSpace(line), got)
		}
	}
}
