// Command event-api serves the event catalog.
package main

import (
	"net/http"

	"ticketmaster-mvp/internal/service"
	"ticketmaster-mvp/services/event/internal/httpapi"
	"ticketmaster-mvp/services/event/internal/postgres"
	"ticketmaster-mvp/services/event/migrations"
)

func main() {
	service.Main(service.Config{
		Name:       "event-api",
		Migrations: migrations.FS,
		Routes: func(mux *http.ServeMux, deps service.Deps) {
			httpapi.NewHandler(postgres.NewStore(deps.Pool)).Register(mux)
		},
	})
}
