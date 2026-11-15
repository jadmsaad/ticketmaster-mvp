// Package postgres is event's Postgres adapter: queries live in queries.sql,
// and sqlc generates the eventdb package from them.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"ticketmaster-mvp/services/event/internal/event"
	"ticketmaster-mvp/services/event/internal/postgres/eventdb"
)

// Store is the event repository.
type Store struct {
	q *eventdb.Queries
}

func NewStore(db eventdb.DBTX) *Store {
	return &Store{q: eventdb.New(db)}
}

func (s *Store) ListEvents(ctx context.Context) ([]event.Event, error) {
	rows, err := s.q.ListEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	events := make([]event.Event, len(rows))
	for i, row := range rows {
		events[i] = toEvent(row)
	}
	return events, nil
}

func (s *Store) GetEvent(ctx context.Context, id int64) (event.Event, error) {
	row, err := s.q.GetEvent(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return event.Event{}, event.ErrNotFound
	}
	if err != nil {
		return event.Event{}, fmt.Errorf("get event %d: %w", id, err)
	}
	return toEvent(row), nil
}

// toEvent converts a row to the domain type. Times are always returned in UTC,
// whatever the server's local time zone.
func toEvent(row eventdb.Event) event.Event {
	return event.Event{ID: row.ID, Name: row.Name, Venue: row.Venue, StartsAt: row.StartsAt.UTC()}
}
