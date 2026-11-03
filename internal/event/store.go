package event

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound means the requested event doesn't exist.
var ErrNotFound = errors.New("event not found")

type Event struct {
	ID       int64     `json:"id"`
	Name     string    `json:"name"`
	Venue    string    `json:"venue"`
	StartsAt time.Time `json:"starts_at"`
}

type Seat struct {
	ID         int64  `json:"id"`
	Section    string `json:"section"`
	Row        string `json:"row"`
	Number     int    `json:"number"`
	PriceCents int    `json:"price_cents"`
	Available  bool   `json:"available"`
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) ListEvents(ctx context.Context) ([]Event, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, venue, starts_at
		FROM events
		ORDER BY starts_at, id`)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Event, error) {
		var e Event
		err := row.Scan(&e.ID, &e.Name, &e.Venue, &e.StartsAt)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	return events, nil
}

func (s *Store) GetEvent(ctx context.Context, id int64) (Event, error) {
	var e Event
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, venue, starts_at
		FROM events
		WHERE id = $1`, id).Scan(&e.ID, &e.Name, &e.Venue, &e.StartsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, fmt.Errorf("query event %d: %w", id, err)
	}
	return e, nil
}

// ListSeats returns an event's seat map. A seat is available if it has no
// booking.
//
// NOT EXISTS rather than a LEFT JOIN: until Task 3, a seat can have several
// bookings, and a join would return that seat once per booking.
func (s *Store) ListSeats(ctx context.Context, eventID int64) ([]Seat, error) {
	if _, err := s.GetEvent(ctx, eventID); err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.section, s.row, s.number, s.price_cents,
		       NOT EXISTS (SELECT 1 FROM bookings b WHERE b.seat_id = s.id)
		FROM seats s
		WHERE s.event_id = $1
		ORDER BY s.section, s.row, s.number`, eventID)
	if err != nil {
		return nil, fmt.Errorf("query seats for event %d: %w", eventID, err)
	}
	seats, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Seat, error) {
		var seat Seat
		err := row.Scan(&seat.ID, &seat.Section, &seat.Row, &seat.Number, &seat.PriceCents, &seat.Available)
		return seat, err
	})
	if err != nil {
		return nil, fmt.Errorf("read seats for event %d: %w", eventID, err)
	}
	return seats, nil
}
