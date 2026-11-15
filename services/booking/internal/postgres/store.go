// Package postgres is booking's Postgres adapter: queries live in
// queries.sql, and sqlc generates the bookingdb package from them.
package postgres

import (
	"context"
	"fmt"

	"ticketmaster-mvp/services/booking/internal/booking"
	"ticketmaster-mvp/services/booking/internal/postgres/bookingdb"
)

// Store is the booking repository: seats and confirmed bookings.
type Store struct {
	q *bookingdb.Queries
}

func NewStore(db bookingdb.DBTX) *Store {
	return &Store{q: bookingdb.New(db)}
}

// ListSeats returns an event's seat map. Booking never asks the event service
// whether the event exists: no inventory means booking.ErrNoInventory.
func (s *Store) ListSeats(ctx context.Context, eventID int64) ([]booking.Seat, error) {
	rows, err := s.q.ListSeats(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("list seats for event %d: %w", eventID, err)
	}
	if len(rows) == 0 {
		return nil, booking.ErrNoInventory
	}
	seats := make([]booking.Seat, len(rows))
	for i, row := range rows {
		seats[i] = booking.Seat{
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
