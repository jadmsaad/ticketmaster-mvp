// Package postgres is booking's Postgres adapter: queries live in
// queries.sql, and sqlc generates the bookingdb package from them.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"ticketmaster-mvp/services/booking/internal/booking"
	"ticketmaster-mvp/services/booking/internal/postgres/bookingdb"
)

// Constraint names from the migrations, used to tell unique violations apart.
const (
	bookingsSeatKey  = "bookings_seat_id_key"
	bookingsOrderKey = "bookings_order_id_key"
)

// Store is the booking repository: seats and confirmed bookings.
type Store struct {
	q *bookingdb.Queries
}

func NewStore(db bookingdb.DBTX) *Store {
	return &Store{q: bookingdb.New(db)}
}

// Layout returns an event's static seat layout, ordered by idx. Booking never
// asks the event service whether the event exists: no inventory means
// booking.ErrNoInventory.
func (s *Store) Layout(ctx context.Context, eventID int64) ([]booking.Seat, error) {
	rows, err := s.q.ListLayout(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("list layout for event %d: %w", eventID, err)
	}
	if len(rows) == 0 {
		return nil, booking.ErrNoInventory
	}
	seats := make([]booking.Seat, len(rows))
	for i, row := range rows {
		seats[i] = booking.Seat{
			Idx:     int(row.Idx),
			Section: row.Section,
			Row:     row.Row,
			Number:  int(row.Number),
		}
	}
	return seats, nil
}

// CreateBooking inserts a booking for (event, seat). The UNIQUE constraints
// make it the final say: a second booking for the seat is booking.ErrSeatTaken.
func (s *Store) CreateBooking(ctx context.Context, b booking.NewBooking) (booking.Booking, error) {
	orderID, err := parseUUID(b.OrderID)
	if err != nil {
		return booking.Booking{}, fmt.Errorf("create booking: %w", err)
	}
	row, err := s.q.CreateBooking(ctx, bookingdb.CreateBookingParams{
		OrderID: orderID,
		UserID:  b.UserID,
		EventID: b.EventID,
		Idx:     int32(b.Seat),
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return booking.Booking{}, booking.ErrSeatNotFound
	case errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == bookingsSeatKey:
		return booking.Booking{}, booking.ErrSeatTaken
	case errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == bookingsOrderKey:
		return booking.Booking{}, booking.ErrOrderAlreadyConfirmed
	case err != nil:
		return booking.Booking{}, fmt.Errorf("create booking for event %d seat %d: %w", b.EventID, b.Seat, err)
	}
	return booking.Booking{
		ID:        row.ID,
		OrderID:   b.OrderID,
		EventID:   b.EventID,
		Seat:      b.Seat,
		UserID:    b.UserID,
		CreatedAt: row.CreatedAt.UTC(),
	}, nil
}

// BookingByOrder returns the booking that confirmed orderID.
func (s *Store) BookingByOrder(ctx context.Context, orderID string) (booking.Booking, error) {
	id, err := parseUUID(orderID)
	if err != nil {
		// Not a valid order ID, so no booking can have it.
		return booking.Booking{}, booking.ErrBookingNotFound
	}
	row, err := s.q.GetBookingByOrder(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return booking.Booking{}, booking.ErrBookingNotFound
	}
	if err != nil {
		return booking.Booking{}, fmt.Errorf("get booking for order %s: %w", orderID, err)
	}
	return booking.Booking{
		ID:        row.ID,
		OrderID:   row.OrderID,
		EventID:   row.EventID,
		Seat:      int(row.Idx),
		UserID:    row.UserID,
		CreatedAt: row.CreatedAt.UTC(),
	}, nil
}

// Inventory returns the event's seat count, sold seats, and per-seat prices,
// for rebuilding Redis seat state.
func (s *Store) Inventory(ctx context.Context, eventID int64) (booking.Inventory, error) {
	priced, err := s.q.ListSeatPrices(ctx, eventID)
	if err != nil {
		return booking.Inventory{}, fmt.Errorf("list seat prices for event %d: %w", eventID, err)
	}
	if len(priced) == 0 {
		return booking.Inventory{}, booking.ErrNoInventory
	}
	inv := booking.Inventory{Seats: len(priced), Prices: make([]int, len(priced))}
	for i, row := range priced {
		// Seat state and the API index seats 0…N-1, so a gap would misplace
		// every later seat's bit and price.
		if int(row.Idx) != i {
			return booking.Inventory{}, fmt.Errorf("event %d: seat idx %d where %d was expected; idx must run 0…N-1", eventID, row.Idx, i)
		}
		inv.Prices[i] = int(row.PriceCents)
	}
	sold, err := s.q.ListSoldSeatIdxs(ctx, eventID)
	if err != nil {
		return booking.Inventory{}, fmt.Errorf("list sold seats for event %d: %w", eventID, err)
	}
	inv.Sold = make([]int, len(sold))
	for i, idx := range sold {
		inv.Sold[i] = int(idx)
	}
	return inv, nil
}

func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid order id %q: %w", s, err)
	}
	return u, nil
}
