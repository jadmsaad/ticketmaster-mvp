// Command event-api serves the event catalog.
package main

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"ticketmaster-mvp/internal/service"
	"ticketmaster-mvp/services/event/internal/httpapi"
	"ticketmaster-mvp/services/event/internal/postgres"
	"ticketmaster-mvp/services/event/migrations"
)

func main() {
	service.Main(service.Config{
		Name:       "event-api",
		Migrations: migrations.FS,
		Routes: func(mux *http.ServeMux, pool *pgxpool.Pool) {
			httpapi.NewHandler(postgres.NewStore(pool)).Register(mux)
		},
	})
}
