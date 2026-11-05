package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// NewPool connects with at most maxConns connections. Callers set this
// explicitly: pgxpool's default, max(4, CPUs), is far too small for a
// service under load, and requests would queue for a connection.
func NewPool(ctx context.Context, databaseURL string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	cfg.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}
	return pool, nil
}

// PoolCollector exports pool stats to Prometheus. Under load, rising empty
// acquires and acquire time mean requests are queuing for a connection, not
// for Postgres.
type PoolCollector struct {
	pool *pgxpool.Pool

	maxConns, acquiredConns, idleConns *prometheus.Desc
	acquireCount, emptyAcquireCount    *prometheus.Desc
	acquireDuration                    *prometheus.Desc
}

func NewPoolCollector(pool *pgxpool.Pool) *PoolCollector {
	d := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc("db_pool_"+name, help, nil, nil)
	}
	return &PoolCollector{
		pool:              pool,
		maxConns:          d("max_conns", "Maximum connections in the pool."),
		acquiredConns:     d("acquired_conns", "Connections currently in use."),
		idleConns:         d("idle_conns", "Connections currently idle."),
		acquireCount:      d("acquires_total", "Connections acquired from the pool."),
		emptyAcquireCount: d("empty_acquires_total", "Acquires that had to wait because no connection was free."),
		acquireDuration:   d("acquire_duration_seconds_total", "Total time spent acquiring connections, including waiting for a free one."),
	}
}

func (c *PoolCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(c, ch)
}

func (c *PoolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(c.maxConns, prometheus.GaugeValue, float64(s.MaxConns()))
	ch <- prometheus.MustNewConstMetric(c.acquiredConns, prometheus.GaugeValue, float64(s.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(c.idleConns, prometheus.GaugeValue, float64(s.IdleConns()))
	ch <- prometheus.MustNewConstMetric(c.acquireCount, prometheus.CounterValue, float64(s.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.emptyAcquireCount, prometheus.CounterValue, float64(s.EmptyAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.acquireDuration, prometheus.CounterValue, s.AcquireDuration().Seconds())
}
