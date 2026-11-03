package event

import (
	"context"
	"errors"
	"testing"
	"time"

	"ticketmaster-mvp/internal/testdb"
	"ticketmaster-mvp/services/event/migrations"
)

func TestGetEvent(t *testing.T) {
	pool := testdb.New(t, "event", migrations.FS)
	store := NewStore(pool)
	ctx := context.Background()

	startsAt := time.Date(2027, time.March, 1, 20, 0, 0, 0, time.UTC)
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO events (name, venue, starts_at)
		VALUES ('Test', 'Hall', $1) RETURNING id`, startsAt).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.GetEvent(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := Event{ID: id, Name: "Test", Venue: "Hall", StartsAt: startsAt}
	if !got.StartsAt.Equal(want.StartsAt) || got.ID != want.ID || got.Name != want.Name || got.Venue != want.Venue {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	if _, err := store.GetEvent(ctx, id+1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown event: got error %v, want ErrNotFound", err)
	}
}
