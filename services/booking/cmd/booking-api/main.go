// Command booking-api serves seat inventory and bookings.
package main

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"ticketmaster-mvp/internal/service"
	"ticketmaster-mvp/services/booking/internal/booking"
	"ticketmaster-mvp/services/booking/migrations"
)

func main() {
	service.Main(service.Config{
		Name:       "booking-api",
		Migrations: migrations.FS,
		Routes: func(mux *http.ServeMux, pool *pgxpool.Pool) {
			booking.NewHandler(booking.NewStore(pool)).Register(mux)
		},
	})
}
