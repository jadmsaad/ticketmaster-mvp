// Package httpapi is booking's HTTP adapter: routes and handlers.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"ticketmaster-mvp/internal/httpx"
	"ticketmaster-mvp/services/booking/internal/booking"
)

// Cache-Control values for the seat map (ADR 12). The layout is static per
// event, so the CDN keeps it for a day. Availability changes every second, so
// each CDN edge asks booking-api at most once a second per event.
const (
	layoutCacheControl       = "public, max-age=86400"
	availabilityCacheControl = "public, max-age=1"
)

// userHeader carries the buyer's ID. It stands in for real authentication,
// which would put a verified user ID here.
const userHeader = "X-User-ID"

// layoutStore is what the handler needs for the static layout (postgres.Store).
type layoutStore interface {
	Layout(ctx context.Context, eventID int64) ([]booking.Seat, error)
}

// orderService is what the handler needs for orders and live seat state
// (booking.Service).
type orderService interface {
	Reserve(ctx context.Context, userID string, eventID int64, seat int) (booking.Order, error)
	Confirm(ctx context.Context, userID, orderID string, eventID int64, seat int) (booking.Order, error)
	Availability(ctx context.Context, eventID int64) (booking.Availability, error)
}

type Handler struct {
	layout layoutStore
	orders orderService
}

func NewHandler(layout layoutStore, orders orderService) *Handler {
	return &Handler{layout: layout, orders: orders}
}

// Register adds the booking routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /events/{id}/layout", h.getLayout)
	mux.HandleFunc("GET /events/{id}/availability", h.getAvailability)
	mux.HandleFunc("POST /orders", h.reserve)
	mux.HandleFunc("POST /orders/{id}/confirm", h.confirm)
}

type layoutResponse struct {
	Seats []booking.Seat `json:"seats"`
}

func (h *Handler) getLayout(w http.ResponseWriter, r *http.Request) {
	eventID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	seats, err := h.layout.Layout(r.Context(), eventID)
	if err != nil {
		writeBookingError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", layoutCacheControl)
	httpx.WriteJSON(w, http.StatusOK, layoutResponse{Seats: seats})
}

// availabilityResponse is the polled half of the seat map (ADR 12). The
// []byte fields are encoded by encoding/json as base64.
type availabilityResponse struct {
	// Seats is the seat count: idx runs 0…Seats-1.
	Seats int `json:"seats"`
	// Unavailable is a bitmap, 1 bit per seat, MSB-first: 1 = sold or held.
	Unavailable []byte `json:"unavailable"`
	// Prices lists the distinct prices in cents.
	Prices []int `json:"prices"`
	// SeatPrice is 1 byte per seat: the index of its price in Prices.
	SeatPrice []byte `json:"seat_price"`
}

func (h *Handler) getAvailability(w http.ResponseWriter, r *http.Request) {
	eventID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	a, err := h.orders.Availability(r.Context(), eventID)
	if err != nil {
		writeBookingError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", availabilityCacheControl)
	httpx.WriteJSON(w, http.StatusOK, availabilityResponse{
		Seats:       a.Seats,
		Unavailable: a.Unavailable(),
		Prices:      a.Prices,
		SeatPrice:   a.SeatPrice,
	})
}

// seatRequest is the body of reserve and confirm: which seat. Seat is a
// pointer so a missing seat can be told apart from seat 0.
type seatRequest struct {
	EventID int64 `json:"event_id"`
	Seat    *int  `json:"seat"`
}

func (h *Handler) reserve(w http.ResponseWriter, r *http.Request) {
	userID, req, ok := readSeatRequest(w, r)
	if !ok {
		return
	}
	order, err := h.orders.Reserve(r.Context(), userID, req.EventID, *req.Seat)
	if err != nil {
		writeBookingError(w, r, err)
		return
	}
	// A retry returns the same order, with the same 201 (ADR 11, amended).
	httpx.WriteJSON(w, http.StatusCreated, order)
}

func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	userID, req, ok := readSeatRequest(w, r)
	if !ok {
		return
	}
	order, err := h.orders.Confirm(r.Context(), userID, r.PathValue("id"), req.EventID, *req.Seat)
	if err != nil {
		writeBookingError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, order)
}

// readSeatRequest reads the buyer and the seat from a reserve or confirm
// request. On failure it has already written the error response.
func readSeatRequest(w http.ResponseWriter, r *http.Request) (string, seatRequest, bool) {
	userID := r.Header.Get(userHeader)
	if userID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "missing "+userHeader+" header")
		return "", seatRequest{}, false
	}
	var req seatRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return "", seatRequest{}, false
	}
	if req.EventID <= 0 || req.Seat == nil || *req.Seat < 0 {
		httpx.WriteError(w, http.StatusBadRequest, "event_id and seat are required")
		return "", seatRequest{}, false
	}
	return userID, req, true
}

// writeBookingError maps a booking error to its HTTP response.
func writeBookingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, booking.ErrNoInventory):
		httpx.WriteError(w, http.StatusNotFound, "no seats for this event")
	case errors.Is(err, booking.ErrSeatNotFound):
		httpx.WriteError(w, http.StatusNotFound, "seat not found")
	case errors.Is(err, booking.ErrHoldNotFound):
		// Expired, someone else's, or never existed: the same answer for all,
		// so an order ID reveals nothing to another user.
		httpx.WriteError(w, http.StatusNotFound, "order not found or expired")
	case errors.Is(err, booking.ErrSeatHeld):
		httpx.WriteError(w, http.StatusConflict, "seat is held")
	case errors.Is(err, booking.ErrSeatTaken):
		httpx.WriteError(w, http.StatusConflict, "seat already booked")
	case errors.Is(err, booking.ErrSeatStateUnavailable):
		// Fail closed (ADR 11): tell the client to retry shortly, never guess.
		slog.Warn("seat state unavailable", "request_id", httpx.RequestID(r.Context()), "method", r.Method, "path", r.URL.Path, "error", err)
		w.Header().Set("Retry-After", "1")
		httpx.WriteError(w, http.StatusServiceUnavailable, "seat state unavailable, retry shortly")
	default:
		httpx.InternalError(w, r, err)
	}
}
