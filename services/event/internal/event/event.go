// Package event is the event catalog's domain: its types and errors. It
// imports no infrastructure; adapters (postgres, httpapi) depend on it, never
// the reverse.
package event

import (
	"errors"
	"time"
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
