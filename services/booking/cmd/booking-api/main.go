// Command booking-api serves seat maps and orders.
package main

import (
	"net/http"

	"ticketmaster-mvp/internal/service"
	"ticketmaster-mvp/services/booking/internal/booking"
	"ticketmaster-mvp/services/booking/internal/httpapi"
	"ticketmaster-mvp/services/booking/internal/postgres"
	"ticketmaster-mvp/services/booking/internal/redis"
	"ticketmaster-mvp/services/booking/migrations"
)

// main is the composition root: it builds the adapters and hands them to the
// domain service and the handler, which only know them through interfaces.
func main() {
	service.Main(service.Config{
		Name:       "booking-api",
		Migrations: migrations.FS,
		Redis:      true,
		Routes: func(mux *http.ServeMux, deps service.Deps) {
			store := postgres.NewStore(deps.Pool)
			orders := booking.NewService(redis.NewSeatState(deps.Redis), store, booking.DefaultHoldTTL)
			httpapi.NewHandler(store, orders).Register(mux)
		},
	})
}
