// Package service runs a service binary: it connects to Postgres, then either
// applies migrations or serves HTTP until stopped.
//
//	booking-api           serve HTTP
//	booking-api migrate   apply migrations, then exit
//
// Migrations run as a separate one-off job before deploys, never at server
// startup, so many replicas don't race to change the schema.
package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ticketmaster-mvp/internal/httpx"
	"ticketmaster-mvp/internal/postgres"
)

type Config struct {
	Name       string
	Migrations fs.FS
	// Routes registers the service's endpoints.
	Routes func(mux *http.ServeMux, pool *pgxpool.Pool)
}

// Main runs the service and exits the process when it stops.
func Main(cfg Config) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", cfg.Name)
	slog.SetDefault(logger)

	if err := run(cfg, os.Args[1:]); err != nil {
		slog.Error("stopped", "error", err)
		os.Exit(1)
	}
}

func run(cfg Config, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is not set")
	}

	switch {
	case len(args) == 0:
		return serve(ctx, cfg, databaseURL)
	case len(args) == 1 && args[0] == "migrate":
		if err := postgres.Migrate(ctx, databaseURL, cfg.Migrations); err != nil {
			return err
		}
		slog.Info("migrations applied")
		return nil
	default:
		return fmt.Errorf("unknown arguments %q (usage: %s [migrate])", args, cfg.Name)
	}
}

func serve(ctx context.Context, cfg Config, databaseURL string) error {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("create connection pool: %w", err)
	}
	defer pool.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httpx.Health(pool.Ping))
	cfg.Routes(mux, pool)

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr)
		serveErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	// Let in-flight requests finish before exiting.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}
