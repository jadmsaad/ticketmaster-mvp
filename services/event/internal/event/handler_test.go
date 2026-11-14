package event

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"ticketmaster-mvp/internal/httpx/httpxtest"
)

// fakeStore lets each test plug in the store behaviour it needs. A method
// whose function isn't set panics, so unexpected store calls fail the test.
type fakeStore struct {
	listEventsFn func(ctx context.Context) ([]Event, error)
	getEventFn   func(ctx context.Context, id int64) (Event, error)
}

func (f fakeStore) ListEvents(ctx context.Context) ([]Event, error) {
	if f.listEventsFn == nil {
		panic("fakeStore: unexpected call to ListEvents")
	}
	return f.listEventsFn(ctx)
}

func (f fakeStore) GetEvent(ctx context.Context, id int64) (Event, error) {
	if f.getEventFn == nil {
		panic("fakeStore: unexpected call to GetEvent")
	}
	return f.getEventFn(ctx, id)
}

var (
	testEvent = Event{
		ID:       1,
		Name:     "The Goroutines: Live",
		Venue:    "Demo Arena",
		StartsAt: time.Date(2027, 3, 1, 20, 0, 0, 0, time.UTC),
	}
	errDB = errors.New("connection refused")
)

func TestHandlerListEvents(t *testing.T) {
	tests := []struct {
		name         string
		listEventsFn func(context.Context) ([]Event, error)
		wantStatus   int
		wantBody     string
	}{
		{
			name:         "lists events",
			listEventsFn: func(context.Context) ([]Event, error) { return []Event{testEvent}, nil },
			wantStatus:   http.StatusOK,
			wantBody: `[{
				"id":        1,
				"name":      "The Goroutines: Live",
				"venue":     "Demo Arena",
				"starts_at": "2027-03-01T20:00:00Z"
			}]`,
		},
		{
			name:         "store fails",
			listEventsFn: func(context.Context) ([]Event, error) { return nil, errDB },
			wantStatus:   http.StatusInternalServerError,
			wantBody:     `{"error": "internal server error"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(fakeStore{listEventsFn: tc.listEventsFn})

			rec := httpxtest.Serve(t, h.Register, http.MethodGet, "/events")

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			httpxtest.AssertJSON(t, rec.Body.Bytes(), tc.wantBody)
		})
	}
}

func TestHandlerGetEvent(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		getEventFn func(context.Context, int64) (Event, error) // nil: the store must not be called
		wantStatus int
		wantBody   string
	}{
		{
			name:       "found",
			path:       "/events/1",
			getEventFn: returns(testEvent),
			wantStatus: http.StatusOK,
			wantBody: `{
				"id":        1,
				"name":      "The Goroutines: Live",
				"venue":     "Demo Arena",
				"starts_at": "2027-03-01T20:00:00Z"
			}`,
		},
		{
			name:       "not found",
			path:       "/events/1",
			getEventFn: fails(ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error": "event not found"}`,
		},
		{
			name:       "store fails",
			path:       "/events/1",
			getEventFn: fails(errDB),
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error": "internal server error"}`,
		},
		{
			name:       "id is not a number",
			path:       "/events/abc",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error": "invalid event id"}`,
		},
		{
			name:       "id is not positive",
			path:       "/events/0",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error": "invalid event id"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var fake fakeStore
			var gotID int64
			if tc.getEventFn != nil {
				// Record the id the handler passes, then answer as the row says.
				fake.getEventFn = func(ctx context.Context, id int64) (Event, error) {
					gotID = id
					return tc.getEventFn(ctx, id)
				}
			}

			rec := httpxtest.Serve(t, NewHandler(fake).Register, http.MethodGet, tc.path)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			httpxtest.AssertJSON(t, rec.Body.Bytes(), tc.wantBody)
			if tc.getEventFn != nil && gotID != 1 {
				t.Errorf("store got id %d, want 1 from the path", gotID)
			}
		})
	}
}

// returns is a getEventFn that always finds e.
func returns(e Event) func(context.Context, int64) (Event, error) {
	return func(context.Context, int64) (Event, error) { return e, nil }
}

// fails is a getEventFn that always fails with err.
func fails(err error) func(context.Context, int64) (Event, error) {
	return func(context.Context, int64) (Event, error) { return Event{}, err }
}
