// Package httpapi is event's HTTP adapter: routes and handlers.
package httpapi

import (
	"context"
	"errors"
	"net/http"

	"ticketmaster-mvp/internal/httpx"
	"ticketmaster-mvp/services/event/internal/event"
)

// eventStore is what the handler needs from the store. postgres.Store satisfies it in
// production; tests pass a fake.
type eventStore interface {
	ListEvents(ctx context.Context) ([]event.Event, error)
	GetEvent(ctx context.Context, id int64) (event.Event, error)
}

type Handler struct {
	store eventStore
}

func NewHandler(store eventStore) *Handler {
	return &Handler{store: store}
}

// Register adds the event routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /events", h.listEvents)
	mux.HandleFunc("GET /events/{id}", h.getEvent)
}

func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) {
	events, err := h.store.ListEvents(r.Context())
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, events)
}

func (h *Handler) getEvent(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid event id")
		return
	}
	ev, err := h.store.GetEvent(r.Context(), id)
	if errors.Is(err, event.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ev)
}
