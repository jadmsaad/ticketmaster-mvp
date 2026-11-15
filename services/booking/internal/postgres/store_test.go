package postgres

import (
	"context"
	"errors"
	"testing"

	"ticketmaster-mvp/internal/testdb"
	"ticketmaster-mvp/services/booking/internal/booking"
	"ticketmaster-mvp/services/booking/migrations"
)

func TestListSeats(t *testing.T) {
	pool := testdb.New(t, "booking", migrations.FS)
	store := NewStore(pool)
	ctx := context.Background()
	const eventID = 42

	var free, booked, doubleBooked int64
	for number, id := range []*int64{&free, &booked, &doubleBooked} {
		err := pool.QueryRow(ctx, `
			INSERT INTO seats (event_id, section, row, number, price_cents)
			VALUES ($1, 'A', '1', $2, 5000) RETURNING id`, eventID, number+1).Scan(id)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, seatID := range []int64{booked, doubleBooked, doubleBooked} {
		_, err := pool.Exec(ctx, `INSERT INTO bookings (seat_id, user_id) VALUES ($1, 'u')`, seatID)
		if err != nil {
			t.Fatal(err)
		}
	}

	seats, err := store.ListSeats(ctx, eventID)
	if err != nil {
		t.Fatal(err)
	}

	// The double-booked seat must appear once, not once per booking.
	if len(seats) != 3 {
		t.Fatalf("got %d seats, want 3: %+v", len(seats), seats)
	}
	want := map[int64]bool{free: true, booked: false, doubleBooked: false}
	for _, seat := range seats {
		if seat.Available != want[seat.ID] {
			t.Errorf("seat %d: available = %v, want %v", seat.ID, seat.Available, want[seat.ID])
		}
	}
}

func TestListSeatsNoInventory(t *testing.T) {
	store := NewStore(testdb.New(t, "booking", migrations.FS))
	_, err := store.ListSeats(context.Background(), 999)
	if !errors.Is(err, booking.ErrNoInventory) {
		t.Fatalf("got error %v, want ErrNoInventory", err)
	}
}
