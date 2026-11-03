// Package booking serves seat inventory and bookings.
package booking

import (
	"errors"
	"net/http"

	"ticketmaster-mvp/internal/httpx"
)

type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler {
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
	if errors.Is(err, ErrNoInventory) {
		httpx.WriteError(w, http.StatusNotFound, "no seats for this event")
		return
	}
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, seats)
}
