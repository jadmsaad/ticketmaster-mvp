// Command booking-api serves seat inventory and bookings.
package main

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"ticketmaster-mvp/internal/service"
	"ticketmaster-mvp/services/booking/internal/httpapi"
	"ticketmaster-mvp/services/booking/internal/postgres"
	"ticketmaster-mvp/services/booking/migrations"
)

func main() {
	service.Main(service.Config{
		Name:       "booking-api",
		Migrations: migrations.FS,
		Routes: func(mux *http.ServeMux, pool *pgxpool.Pool) {
			httpapi.NewHandler(postgres.NewStore(pool)).Register(mux)
		},
	})
}
