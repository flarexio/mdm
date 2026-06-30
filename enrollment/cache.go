package enrollment

import "time"

// Cache is a shared, short-lived, strongly-consistent view of enrollments that
// bridges the eventual-consistency lag of the durable Repository. The durable
// store is written asynchronously by the event handler, so Authenticate writes
// through here to give the immediately-following first TokenUpdate a read-your-
// write path across instances. Entries expire once the durable store has caught up.
type Cache interface {
	Store(e *Enrollment, ttl time.Duration) error
	Find(id ID) (*Enrollment, error)
	Close() error
}
