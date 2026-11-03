package event

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"ticketmaster-mvp/internal/event/eventdb"
)

// ErrNotFound means the requested event doesn't exist.
var ErrNotFound = errors.New("event not found")

// Event and Seat are the API's types. They are kept separate from the
// sqlc-generated row types so a schema change doesn't silently change the API.
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

// Store is the event repository. Queries live in queries.sql; sqlc generates
// the eventdb package from them.
type Store struct {
	q *eventdb.Queries
}

func NewStore(db eventdb.DBTX) *Store {
	return &Store{q: eventdb.New(db)}
}

func (s *Store) ListEvents(ctx context.Context) ([]Event, error) {
	rows, err := s.q.ListEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	events := make([]Event, len(rows))
	for i, row := range rows {
		events[i] = toEvent(row)
	}
	return events, nil
}

func (s *Store) GetEvent(ctx context.Context, id int64) (Event, error) {
	row, err := s.q.GetEvent(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, fmt.Errorf("get event %d: %w", id, err)
	}
	return toEvent(row), nil
}

// ListSeats returns an event's seat map, or ErrNotFound if the event doesn't
// exist.
func (s *Store) ListSeats(ctx context.Context, eventID int64) ([]Seat, error) {
	if _, err := s.GetEvent(ctx, eventID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListSeats(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("list seats for event %d: %w", eventID, err)
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

func toEvent(row eventdb.Event) Event {
	return Event{ID: row.ID, Name: row.Name, Venue: row.Venue, StartsAt: row.StartsAt}
}
