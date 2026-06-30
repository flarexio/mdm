package inmem

import (
	"sync"
	"time"

	"github.com/flarexio/core/events"
	"github.com/flarexio/mdm/enrollment"
)

// NewEnrollmentCache returns an in-memory enrollment.Cache for single-node use; a
// scaled deployment swaps in the Redis implementation behind the same interface.
func NewEnrollmentCache() (enrollment.Cache, error) {
	return &enrollmentCache{entries: make(map[enrollment.ID]cacheEntry)}, nil
}

type cacheEntry struct {
	enrollment *enrollment.Enrollment
	expiresAt  time.Time
}

type enrollmentCache struct {
	sync.RWMutex
	entries map[enrollment.ID]cacheEntry
}

func (c *enrollmentCache) Store(e *enrollment.Enrollment, ttl time.Duration) error {
	c.Lock()
	defer c.Unlock()

	// Copy without the event store: events are an in-flight concern, not state.
	stored := new(enrollment.Enrollment)
	*stored = *e
	stored.EventStore = nil

	c.entries[stored.ID] = cacheEntry{enrollment: stored, expiresAt: time.Now().Add(ttl)}
	return nil
}

func (c *enrollmentCache) Find(id enrollment.ID) (*enrollment.Enrollment, error) {
	c.RLock()
	entry, ok := c.entries[id]
	c.RUnlock()

	if !ok {
		return nil, enrollment.ErrEnrollmentNotFound
	}

	if time.Now().After(entry.expiresAt) {
		c.Lock()
		delete(c.entries, id)
		c.Unlock()
		return nil, enrollment.ErrEnrollmentNotFound
	}

	entry.enrollment.EventStore = events.NewEventStore()
	return entry.enrollment, nil
}

func (c *enrollmentCache) Close() error { return nil }
