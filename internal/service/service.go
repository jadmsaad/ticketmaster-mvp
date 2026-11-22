// Package service runs a service binary: it connects to Postgres, then either
// applies migrations or serves HTTP until stopped.
//
//	booking-api           serve HTTP
//	booking-api migrate   apply migrations, then exit
//
// Migrations run as a separate one-off job before deploys, never at server
// startup, so many replicas don't race to change the schema.
//
// Environment:
//
//	DATABASE_URL   required
//	REDIS_URL      required by services that set Config.Redis
//	ADDR           listen address (default :8080)
//	DB_MAX_CONNS   connection pool size (default 20)
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
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	goredis "github.com/redis/go-redis/v9"

	"ticketmaster-mvp/internal/httpx"
	"ticketmaster-mvp/internal/postgres"
)

type Config struct {
	Name       string
	Migrations fs.FS
	// Redis makes the service connect to REDIS_URL at startup.
	Redis bool
	// Routes registers the service's endpoints.
	Routes func(mux *http.ServeMux, deps Deps)
}

// Deps are the connections a service's routes can use.
type Deps struct {
	Pool *pgxpool.Pool
	// Redis is nil unless Config.Redis is set.
	Redis *goredis.Client
}

// env is the runtime configuration read from environment variables.
type env struct {
	databaseURL string
	redisURL    string
	addr        string
	maxConns    int32
}

func loadEnv() (env, error) {
	e := env{
		databaseURL: os.Getenv("DATABASE_URL"),
		redisURL:    os.Getenv("REDIS_URL"),
		addr:        os.Getenv("ADDR"),
		maxConns:    20,
	}
	if e.databaseURL == "" {
		return env{}, errors.New("DATABASE_URL is not set")
	}
	if e.addr == "" {
		e.addr = ":8080"
	}
	if v := os.Getenv("DB_MAX_CONNS"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 {
			return env{}, fmt.Errorf("DB_MAX_CONNS must be a positive integer, got %q", v)
		}
		e.maxConns = int32(n)
	}
	return e, nil
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

	e, err := loadEnv()
	if err != nil {
		return err
	}

	switch {
	case len(args) == 0:
		return serve(ctx, cfg, e)
	case len(args) == 1 && args[0] == "migrate":
		if err := postgres.Migrate(ctx, e.databaseURL, cfg.Migrations); err != nil {
			return err
		}
		slog.Info("migrations applied")
		return nil
	default:
		return fmt.Errorf("unknown arguments %q (usage: %s [migrate])", args, cfg.Name)
	}
}

func serve(ctx context.Context, cfg Config, e env) error {
	pool, err := postgres.NewPool(ctx, e.databaseURL, e.maxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	prometheus.MustRegister(postgres.NewPoolCollector(pool))

	deps := Deps{Pool: pool}
	if cfg.Redis {
		rdb, err := connectRedis(ctx, e.redisURL)
		if err != nil {
			return err
		}
		defer func() { _ = rdb.Close() }()
		deps.Redis = rdb
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httpx.Health(pool.Ping))
	// Internal only: the load balancer routes just the API paths, so this
	// isn't reachable from the internet.
	mux.Handle("GET /metrics", promhttp.Handler())
	cfg.Routes(mux, deps)

	server := &http.Server{
		Addr:    e.addr,
		Handler: httpx.Observe(mux),
		// Bound how long one slow or idle client can hold a connection.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", e.addr, "db_max_conns", e.maxConns)
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

// connectRedis opens a Redis client and checks it can reach the server, so a
// wrong REDIS_URL stops the service at startup rather than at the first
// request.
func connectRedis(ctx context.Context, url string) (*goredis.Client, error) {
	if url == "" {
		return nil, errors.New("REDIS_URL is not set")
	}
	opts, err := goredis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	rdb := goredis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("connect to redis: %w", err)
	}
	return rdb, nil
}
