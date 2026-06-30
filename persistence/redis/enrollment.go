// Package redis is the shared, Redis-backed enrollment.Cache: the cross-instance
// bridge over the durable repository's eventual-consistency lag. See enrollment.Cache.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/flarexio/core/events"
	"github.com/flarexio/mdm/enrollment"
)

const enrollmentPrefix = "mdm:enrollment:"

type enrollmentCache struct {
	rdb *redis.Client
	ctx context.Context
}

// NewEnrollmentCache wraps a shared Redis client (the one every instance points at).
func NewEnrollmentCache(rdb *redis.Client) (enrollment.Cache, error) {
	return &enrollmentCache{rdb: rdb, ctx: context.Background()}, nil
}

// Open dials addr and returns the cache, failing fast if Redis is unreachable.
func Open(addr, password string, db int) (enrollment.Cache, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return NewEnrollmentCache(rdb)
}

func key(id enrollment.ID) string { return enrollmentPrefix + id.String() }

func (c *enrollmentCache) Store(e *enrollment.Enrollment, ttl time.Duration) error {
	// EventStore is dropped by its json:"-" tag — only aggregate state is cached.
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}

	return c.rdb.Set(c.ctx, key(e.ID), data, ttl).Err()
}

func (c *enrollmentCache) Find(id enrollment.ID) (*enrollment.Enrollment, error) {
	data, err := c.rdb.Get(c.ctx, key(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, enrollment.ErrEnrollmentNotFound
	}
	if err != nil {
		return nil, err
	}

	var e enrollment.Enrollment
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}

	e.EventStore = events.NewEventStore()
	return &e, nil
}

func (c *enrollmentCache) Close() error { return c.rdb.Close() }
