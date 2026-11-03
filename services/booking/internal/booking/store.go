package booking

import (
	"context"
	"errors"
	"fmt"

	"ticketmaster-mvp/services/booking/internal/booking/bookingdb"
)

// ErrNoInventory means the event has no seats in booking's inventory, either
// because the event doesn't exist or because it isn't on sale.
var ErrNoInventory = errors.New("no seat inventory for event")

// Seat is the API's type, kept separate from the sqlc-generated row type.
type Seat struct {
	ID         int64  `json:"id"`
	Section    string `json:"section"`
	Row        string `json:"row"`
	Number     int    `json:"number"`
	PriceCents int    `json:"price_cents"`
	Available  bool   `json:"available"`
}

// Store is the booking repository. Queries live in queries.sql; sqlc generates
// the bookingdb package from them.
type Store struct {
	q *bookingdb.Queries
}

func NewStore(db bookingdb.DBTX) *Store {
	return &Store{q: bookingdb.New(db)}
}

// ListSeats returns an event's seat map. Booking never asks the event service
// whether the event exists: no inventory means ErrNoInventory.
func (s *Store) ListSeats(ctx context.Context, eventID int64) ([]Seat, error) {
	rows, err := s.q.ListSeats(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("list seats for event %d: %w", eventID, err)
	}
	if len(rows) == 0 {
		return nil, ErrNoInventory
	}
	seats := make([]Seat, len(rows))
	for i, row := range rows {
		seats[i] = Seat{
			ID:         row.ID,
			Section:    row.Section,
			Row:        row.Row,
			Number:     int(row.Number),
			PriceCents: int(row.PriceCents),
			Available:  row.Available,
		}
	}
	return seats, nil
}
