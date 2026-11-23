package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ticketmaster-mvp/internal/httpx/httpxtest"
	"ticketmaster-mvp/services/booking/internal/booking"
)

// fakeLayout and fakeOrders let each test plug in the behaviour it needs. A
// method whose function isn't set panics, so unexpected calls fail the test.
type fakeLayout struct {
	layoutFn func(ctx context.Context, eventID int64) ([]booking.Seat, error)
}

func (f fakeLayout) Layout(ctx context.Context, eventID int64) ([]booking.Seat, error) {
	if f.layoutFn == nil {
		panic("fakeLayout: unexpected call to Layout")
	}
	return f.layoutFn(ctx, eventID)
}

type fakeOrders struct {
	reserveFn      func(ctx context.Context, userID string, eventID int64, seat int) (booking.Order, error)
	confirmFn      func(ctx context.Context, userID, orderID string, eventID int64, seat int) (booking.Order, error)
	availabilityFn func(ctx context.Context, eventID int64) (booking.Availability, error)
}

func (f fakeOrders) Reserve(ctx context.Context, userID string, eventID int64, seat int) (booking.Order, error) {
	if f.reserveFn == nil {
		panic("fakeOrders: unexpected call to Reserve")
	}
	return f.reserveFn(ctx, userID, eventID, seat)
}

func (f fakeOrders) Confirm(ctx context.Context, userID, orderID string, eventID int64, seat int) (booking.Order, error) {
	if f.confirmFn == nil {
		panic("fakeOrders: unexpected call to Confirm")
	}
	return f.confirmFn(ctx, userID, orderID, eventID, seat)
}

func (f fakeOrders) Availability(ctx context.Context, eventID int64) (booking.Availability, error) {
	if f.availabilityFn == nil {
		panic("fakeOrders: unexpected call to Availability")
	}
	return f.availabilityFn(ctx, eventID)
}

var errDown = errors.New("connection refused")

// errRedisDown is how the Redis adapter reports an unreachable server.
var errRedisDown = fmt.Errorf("reserve: %w: %w", booking.ErrSeatStateUnavailable, errDown)

func TestHandlerLayout(t *testing.T) {
	seats := []booking.Seat{
		{Idx: 0, Section: "A", Row: "1", Number: 1},
		{Idx: 1, Section: "A", Row: "1", Number: 2},
	}
	tests := []struct {
		name      string
		path      string
		layoutFn  func(context.Context, int64) ([]booking.Seat, error) // nil: must not be called
		wantCode  int
		wantBody  string
		wantCache string
	}{
		{
			name:     "layout",
			path:     "/events/7/layout",
			layoutFn: func(context.Context, int64) ([]booking.Seat, error) { return seats, nil },
			wantCode: http.StatusOK,
			wantBody: `{"seats": [
				{"idx": 0, "section": "A", "row": "1", "number": 1},
				{"idx": 1, "section": "A", "row": "1", "number": 2}
			]}`,
			wantCache: "public, max-age=86400",
		},
		{
			name:     "no inventory",
			path:     "/events/7/layout",
			layoutFn: func(context.Context, int64) ([]booking.Seat, error) { return nil, booking.ErrNoInventory },
			wantCode: http.StatusNotFound,
			wantBody: `{"error": "no seats for this event"}`,
		},
		{
			name:     "store fails",
			path:     "/events/7/layout",
			layoutFn: func(context.Context, int64) ([]booking.Seat, error) { return nil, errDown },
			wantCode: http.StatusInternalServerError,
			wantBody: `{"error": "internal server error"}`,
		},
		{
			name:     "invalid event id",
			path:     "/events/abc/layout",
			wantCode: http.StatusBadRequest,
			wantBody: `{"error": "invalid event id"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var fake fakeLayout
			var gotID int64
			if tc.layoutFn != nil {
				// Record the event id the handler passes, then answer as the row says.
				fake.layoutFn = func(ctx context.Context, eventID int64) ([]booking.Seat, error) {
					gotID = eventID
					return tc.layoutFn(ctx, eventID)
				}
			}

			rec := httpxtest.Serve(t, NewHandler(fake, fakeOrders{}).Register, http.MethodGet, tc.path)

			assertResponse(t, rec, tc.wantCode, tc.wantBody)
			if got := rec.Header().Get("Cache-Control"); got != tc.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, tc.wantCache)
			}
			if tc.layoutFn != nil && gotID != 7 {
				t.Errorf("store got event id %d, want 7 from the path", gotID)
			}
		})
	}
}

func TestHandlerAvailability(t *testing.T) {
	// 10 seats: seat 0 sold, seat 4 held; seats 0–4 cost $50, seats 5–9 $90.
	a := booking.Availability{
		Seats:     10,
		Sold:      []byte{0b1000_0000},
		Held:      []int{4},
		Prices:    []int{5000, 9000},
		SeatPrice: []byte{0, 0, 0, 0, 0, 1, 1, 1, 1, 1},
	}
	tests := []struct {
		name           string
		availabilityFn func(context.Context, int64) (booking.Availability, error)
		wantCode       int
		wantBody       string
		wantHeader     map[string]string
	}{
		{
			name:           "sold and held seats both unavailable, prices dictionary-encoded",
			availabilityFn: func(context.Context, int64) (booking.Availability, error) { return a, nil },
			wantCode:       http.StatusOK,
			// "iAA=" is 1000_1000 0000_0000: seats 0 and 4.
			// "AAAAAAABAQEBAQ==" is bytes 0,0,0,0,0,1,1,1,1,1.
			wantBody: `{
				"seats": 10,
				"unavailable": "iAA=",
				"prices": [5000, 9000],
				"seat_price": "AAAAAAABAQEBAQ=="
			}`,
			wantHeader: map[string]string{"Cache-Control": "public, max-age=1"},
		},
		{
			name: "no inventory",
			availabilityFn: func(context.Context, int64) (booking.Availability, error) {
				return booking.Availability{}, booking.ErrNoInventory
			},
			wantCode: http.StatusNotFound,
			wantBody: `{"error": "no seats for this event"}`,
		},
		{
			name: "Redis unreachable fails closed",
			availabilityFn: func(context.Context, int64) (booking.Availability, error) {
				return booking.Availability{}, errRedisDown
			},
			wantCode:   http.StatusServiceUnavailable,
			wantBody:   `{"error": "seat state unavailable, retry shortly"}`,
			wantHeader: map[string]string{"Retry-After": "1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orders := fakeOrders{availabilityFn: tc.availabilityFn}

			rec := httpxtest.Serve(t, NewHandler(fakeLayout{}, orders).Register, http.MethodGet, "/events/7/availability")

			assertResponse(t, rec, tc.wantCode, tc.wantBody)
			for name, want := range tc.wantHeader {
				if got := rec.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}

// heldOrder is what the fake service returns for a successful reserve.
var heldOrder = booking.Order{
	ID: "ord-1", EventID: 7, Seat: 5, UserID: "alice", Status: booking.StatusHeld,
	ExpiresAt: time.Date(2027, 3, 1, 20, 10, 0, 0, time.UTC),
}

func TestHandlerReserve(t *testing.T) {
	tests := []struct {
		name      string
		user      string
		body      string
		reserveFn func(context.Context, string, int64, int) (booking.Order, error) // nil: must not be called
		wantCode  int
		wantBody  string
	}{
		{
			name:      "reserved",
			user:      "alice",
			body:      `{"event_id": 7, "seat": 5}`,
			reserveFn: func(context.Context, string, int64, int) (booking.Order, error) { return heldOrder, nil },
			wantCode:  http.StatusCreated,
			wantBody: `{"id": "ord-1", "event_id": 7, "seat": 5, "user_id": "alice",
				"status": "held", "expires_at": "2027-03-01T20:10:00Z"}`,
		},
		{
			name:      "seat 0 is a real seat",
			user:      "alice",
			body:      `{"event_id": 7, "seat": 0}`,
			reserveFn: func(context.Context, string, int64, int) (booking.Order, error) { return heldOrder, nil },
			wantCode:  http.StatusCreated,
			wantBody: `{"id": "ord-1", "event_id": 7, "seat": 5, "user_id": "alice",
				"status": "held", "expires_at": "2027-03-01T20:10:00Z"}`,
		},
		{name: "no user", body: `{"event_id": 7, "seat": 5}`, wantCode: http.StatusUnauthorized, wantBody: `{"error": "missing X-User-ID header"}`},
		{name: "not JSON", user: "alice", body: `seat=5`, wantCode: http.StatusBadRequest, wantBody: `{"error": "invalid request body"}`},
		{name: "unknown field", user: "alice", body: `{"event_id": 7, "seat": 5, "qty": 2}`, wantCode: http.StatusBadRequest, wantBody: `{"error": "invalid request body"}`},
		{name: "missing seat", user: "alice", body: `{"event_id": 7}`, wantCode: http.StatusBadRequest, wantBody: `{"error": "event_id and seat are required"}`},
		{name: "negative seat", user: "alice", body: `{"event_id": 7, "seat": -1}`, wantCode: http.StatusBadRequest, wantBody: `{"error": "event_id and seat are required"}`},
		{name: "missing event", user: "alice", body: `{"seat": 5}`, wantCode: http.StatusBadRequest, wantBody: `{"error": "event_id and seat are required"}`},
		{
			name: "seat held",
			user: "alice",
			body: `{"event_id": 7, "seat": 5}`,
			reserveFn: func(context.Context, string, int64, int) (booking.Order, error) {
				return booking.Order{}, booking.ErrSeatHeld
			},
			wantCode: http.StatusConflict,
			wantBody: `{"error": "seat is held"}`,
		},
		{
			name: "seat sold",
			user: "alice",
			body: `{"event_id": 7, "seat": 5}`,
			reserveFn: func(context.Context, string, int64, int) (booking.Order, error) {
				return booking.Order{}, booking.ErrSeatTaken
			},
			wantCode: http.StatusConflict,
			wantBody: `{"error": "seat already booked"}`,
		},
		{
			name: "no such seat",
			user: "alice",
			body: `{"event_id": 7, "seat": 500}`,
			reserveFn: func(context.Context, string, int64, int) (booking.Order, error) {
				return booking.Order{}, booking.ErrSeatNotFound
			},
			wantCode: http.StatusNotFound,
			wantBody: `{"error": "seat not found"}`,
		},
		{
			name: "no such event",
			user: "alice",
			body: `{"event_id": 7, "seat": 5}`,
			// The service wraps the rebuild's error; errors.Is still finds it.
			reserveFn: func(context.Context, string, int64, int) (booking.Order, error) {
				return booking.Order{}, fmt.Errorf("rebuild seat state for event 7: %w", booking.ErrNoInventory)
			},
			wantCode: http.StatusNotFound,
			wantBody: `{"error": "no seats for this event"}`,
		},
		{
			name:      "Redis unreachable fails closed",
			user:      "alice",
			body:      `{"event_id": 7, "seat": 5}`,
			reserveFn: func(context.Context, string, int64, int) (booking.Order, error) { return booking.Order{}, errRedisDown },
			wantCode:  http.StatusServiceUnavailable,
			wantBody:  `{"error": "seat state unavailable, retry shortly"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var orders fakeOrders
			if tc.reserveFn != nil {
				orders.reserveFn = func(ctx context.Context, userID string, eventID int64, seat int) (booking.Order, error) {
					// The handler passes the header's user and the body's seat through.
					if userID != tc.user || eventID != 7 || !strings.Contains(tc.body, fmt.Sprintf(`"seat": %d`, seat)) {
						t.Errorf("service got user %q, event %d, seat %d", userID, eventID, seat)
					}
					return tc.reserveFn(ctx, userID, eventID, seat)
				}
			}

			rec := httpxtest.Do(t, NewHandler(fakeLayout{}, orders).Register, seatRequestTo(t, "/orders", tc.user, tc.body))

			assertResponse(t, rec, tc.wantCode, tc.wantBody)
		})
	}
}

func TestHandlerConfirm(t *testing.T) {
	confirmed := booking.Order{ID: "ord-1", EventID: 7, Seat: 5, UserID: "alice", Status: booking.StatusConfirmed}
	tests := []struct {
		name      string
		confirmFn func(context.Context, string, string, int64, int) (booking.Order, error)
		wantCode  int
		wantBody  string
	}{
		{
			name:      "confirmed",
			confirmFn: func(context.Context, string, string, int64, int) (booking.Order, error) { return confirmed, nil },
			wantCode:  http.StatusOK,
			// No expires_at once confirmed (omitzero).
			wantBody: `{"id": "ord-1", "event_id": 7, "seat": 5, "user_id": "alice", "status": "confirmed"}`,
		},
		{
			name: "expired, someone else's, or unknown",
			confirmFn: func(context.Context, string, string, int64, int) (booking.Order, error) {
				return booking.Order{}, booking.ErrHoldNotFound
			},
			wantCode: http.StatusNotFound,
			wantBody: `{"error": "order not found or expired"}`,
		},
		{
			name: "backstop: seat sold to another order",
			confirmFn: func(context.Context, string, string, int64, int) (booking.Order, error) {
				return booking.Order{}, booking.ErrSeatTaken
			},
			wantCode: http.StatusConflict,
			wantBody: `{"error": "seat already booked"}`,
		},
		{
			name: "Postgres fails",
			confirmFn: func(context.Context, string, string, int64, int) (booking.Order, error) {
				return booking.Order{}, errDown
			},
			wantCode: http.StatusInternalServerError,
			wantBody: `{"error": "internal server error"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			orders := fakeOrders{confirmFn: func(ctx context.Context, userID, orderID string, eventID int64, seat int) (booking.Order, error) {
				// The order ID comes from the path; the seat from the body.
				if userID != "alice" || orderID != "ord-1" || eventID != 7 || seat != 5 {
					t.Errorf("service got user %q, order %q, event %d, seat %d", userID, orderID, eventID, seat)
				}
				return tc.confirmFn(ctx, userID, orderID, eventID, seat)
			}}
			req := seatRequestTo(t, "/orders/ord-1/confirm", "alice", `{"event_id": 7, "seat": 5}`)

			rec := httpxtest.Do(t, NewHandler(fakeLayout{}, orders).Register, req)

			assertResponse(t, rec, tc.wantCode, tc.wantBody)
		})
	}
}

// seatRequestTo builds a reserve or confirm request. An empty user sends no
// X-User-ID header.
func seatRequestTo(t *testing.T, path, user, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
	if user != "" {
		req.Header.Set("X-User-ID", user)
	}
	return req
}

func assertResponse(t *testing.T, rec *httptest.ResponseRecorder, wantCode int, wantBody string) {
	t.Helper()
	if rec.Code != wantCode {
		t.Errorf("status = %d, want %d", rec.Code, wantCode)
	}
	httpxtest.AssertJSON(t, rec.Body.Bytes(), wantBody)
}
