// Package testredis gives integration tests an empty Redis database.
package testredis

import (
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

// New returns a client for the Redis database in TEST_REDIS_URL, emptied
// first. It skips the test unless TEST_REDIS_URL is set.
//
// Every test flushes the same database, so packages that use it must not run
// in parallel (go test -p 1, as with testdb).
func New(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushDB(t.Context()).Err(); err != nil {
		t.Fatal(err)
	}
	return rdb
}
