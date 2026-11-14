package booking

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"ticketmaster-mvp/internal/httpx/httpxtest"
)

// fakeStore lets each test plug in the store behaviour it needs. A method
// whose function isn't set panics, so unexpected store calls fail the test.
type fakeStore struct {
	listSeatsFn func(ctx context.Context, eventID int64) ([]Seat, error)
}

func (f fakeStore) ListSeats(ctx context.Context, eventID int64) ([]Seat, error) {
	if f.listSeatsFn == nil {
		panic("fakeStore: unexpected call to ListSeats")
	}
	return f.listSeatsFn(ctx, eventID)
}

func TestHandlerListSeats(t *testing.T) {
	seats := []Seat{
		{ID: 1, Section: "A", Row: "1", Number: 1, PriceCents: 5000, Available: true},
		{ID: 2, Section: "A", Row: "1", Number: 2, PriceCents: 5000, Available: false},
	}
	tests := []struct {
		name        string
		path        string
		listSeatsFn func(context.Context, int64) ([]Seat, error) // nil: the store must not be called
		wantStatus  int
		wantBody    string
	}{
		{
			name:        "seat map",
			path:        "/events/7/seats",
			listSeatsFn: func(context.Context, int64) ([]Seat, error) { return seats, nil },
			wantStatus:  http.StatusOK,
			wantBody: `[
				{"id": 1, "section": "A", "row": "1", "number": 1, "price_cents": 5000, "available": true},
				{"id": 2, "section": "A", "row": "1", "number": 2, "price_cents": 5000, "available": false}
			]`,
		},
		{
			name:        "no inventory",
			path:        "/events/7/seats",
			listSeatsFn: func(context.Context, int64) ([]Seat, error) { return nil, ErrNoInventory },
			wantStatus:  http.StatusNotFound,
			wantBody:    `{"error": "no seats for this event"}`,
		},
		{
			name:        "store fails",
			path:        "/events/7/seats",
			listSeatsFn: func(context.Context, int64) ([]Seat, error) { return nil, errors.New("connection refused") },
			wantStatus:  http.StatusInternalServerError,
			wantBody:    `{"error": "internal server error"}`,
		},
		{
			name:       "invalid event id",
			path:       "/events/abc/seats",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error": "invalid event id"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var fake fakeStore
			var gotID int64
			if tc.listSeatsFn != nil {
				// Record the event id the handler passes, then answer as the row says.
				fake.listSeatsFn = func(ctx context.Context, eventID int64) ([]Seat, error) {
					gotID = eventID
					return tc.listSeatsFn(ctx, eventID)
				}
			}

			rec := httpxtest.Serve(t, NewHandler(fake).Register, http.MethodGet, tc.path)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			httpxtest.AssertJSON(t, rec.Body.Bytes(), tc.wantBody)
			if tc.listSeatsFn != nil && gotID != 7 {
				t.Errorf("store got event id %d, want 7 from the path", gotID)
			}
		})
	}
}
