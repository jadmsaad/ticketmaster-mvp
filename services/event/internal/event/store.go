package event

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"ticketmaster-mvp/services/event/internal/event/eventdb"
)

// ErrNotFound means the requested event doesn't exist.
var ErrNotFound = errors.New("event not found")

// Event is the API's type. It is kept separate from the sqlc-generated row
// type so a schema change doesn't silently change the API.
type Event struct {
	ID       int64     `json:"id"`
	Name     string    `json:"name"`
	Venue    string    `json:"venue"`
	StartsAt time.Time `json:"starts_at"`
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

// toEvent converts a row to the API type. Times are always returned in UTC,
// whatever the server's local time zone.
func toEvent(row eventdb.Event) Event {
	return Event{ID: row.ID, Name: row.Name, Venue: row.Venue, StartsAt: row.StartsAt.UTC()}
}
