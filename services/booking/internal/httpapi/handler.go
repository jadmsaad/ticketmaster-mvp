// Package httpapi is booking's HTTP adapter: routes and handlers.
package httpapi

import (
	"context"
	"errors"
	"net/http"

	"ticketmaster-mvp/internal/httpx"
	"ticketmaster-mvp/services/booking/internal/booking"
)

// seatStore is what the handler needs from the store. postgres.Store satisfies it in
// production; tests pass a fake.
type seatStore interface {
	ListSeats(ctx context.Context, eventID int64) ([]booking.Seat, error)
}

type Handler struct {
	store seatStore
}

func NewHandler(store seatStore) *Handler {
	return &Handler{store: store}
}

// Register adds the booking routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /events/{id}/seats", h.listSeats)
}

func (h *Handler) listSeats(w http.ResponseWriter, r *http.Request) {
	eventID, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	seats, err := h.store.ListSeats(r.Context(), eventID)
	if errors.Is(err, booking.ErrNoInventory) {
		httpx.WriteError(w, http.StatusNotFound, "no seats for this event")
		return
	}
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, seats)
}
