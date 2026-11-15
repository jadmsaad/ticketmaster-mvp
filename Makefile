# Day-to-day dev: Postgres runs in Docker, the services run natively with go run.
#
#   make setup         start Postgres, apply migrations, seed sample data
#   make run-event     event-api on :8081
#   make run-booking   booking-api on :8082
#
# Local-only credentials, matching deploy/postgres/init.sql.

PG          := postgres://%s@localhost:5432/ticketmaster?sslmode=disable
EVENT_DB    := $(subst %s,event_api:event_api,$(PG))
BOOKING_DB  := $(subst %s,booking_api:booking_api,$(PG))
ADMIN_DB    := $(subst %s,ticketmaster:ticketmaster,$(PG))
TEST_DB     := postgres://ticketmaster:ticketmaster@localhost:5432/postgres?sslmode=disable
TEST_REDIS  := redis://localhost:6379/15

EVENT_CMD   := ./services/event/cmd/event-api
BOOKING_CMD := ./services/booking/cmd/booking-api

# Keep in sync with .github/workflows/ci.yml.
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
GOVULNCHECK   := go run golang.org/x/vuln/cmd/govulncheck@v1.9.0

.PHONY: setup db migrate seed reset run-event run-booking test lint ci generate up-all down

setup: db migrate seed ## Start Postgres, migrate, and seed

db: ## Start Postgres and Redis, and wait until they're healthy
	docker compose up -d --wait db redis

migrate: ## Apply both services' migrations
	DATABASE_URL=$(EVENT_DB) go run $(EVENT_CMD) migrate
	DATABASE_URL=$(BOOKING_DB) go run $(BOOKING_CMD) migrate

seed: ## Create sample data (no-op if it already exists)
	DATABASE_URL=$(ADMIN_DB) go run ./cmd/seed

reset: ## Wipe all data, including bookings, and re-seed
	DATABASE_URL=$(ADMIN_DB) go run ./cmd/seed -reset

run-event: ## Run event-api on :8081
	DATABASE_URL=$(EVENT_DB) ADDR=:8081 go run $(EVENT_CMD)

run-booking: ## Run booking-api on :8082
	DATABASE_URL=$(BOOKING_DB) ADDR=:8082 go run $(BOOKING_CMD)

test: ## Run all tests against the local Postgres and Redis, with the race detector
	TEST_POSTGRES_URL=$(TEST_DB) TEST_REDIS_URL=$(TEST_REDIS) go test -race -p 1 ./...

lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

ci: lint test ## Run the CI checks locally (except the Docker builds)
	go build ./...
	go mod tidy && git diff --exit-code -- go.mod go.sum
	go tool sqlc diff
	$(GOVULNCHECK) ./...

generate: ## Regenerate sqlc code after changing queries or migrations
	go tool sqlc generate

up-all: ## Run everything in containers, as in production
	docker compose --profile apps up -d --build --wait

down: ## Stop all containers (data is kept)
	docker compose --profile apps down
