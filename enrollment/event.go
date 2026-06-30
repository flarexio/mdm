package enrollment

import (
	"strings"
	"time"

	"github.com/flarexio/core/events"
)

type EventName int

const (
	Unknown EventName = iota
	EnrollmentAuthenticated
	EnrollmentTokenUpdated
	EnrollmentCheckedOut
)

func (name EventName) String() string {
	switch name {
	case EnrollmentAuthenticated:
		return "enrollment_authenticated"
	case EnrollmentTokenUpdated:
		return "enrollment_token_updated"
	case EnrollmentCheckedOut:
		return "enrollment_checked_out"
	default:
		return ""
	}
}

// Event is the shared base for enrollment domain events.
type Event struct {
	Domain       string    `json:"domain"`
	Name         EventName `json:"name"`
	EnrollmentID ID        `json:"enrollment_id"` // AggregateRoot
	OccuredAt    time.Time `json:"occured_at"`
}

func NewEvent(name EventName, e *Enrollment) *Event {
	return &Event{
		Domain:       "mdm:enrollments",
		Name:         name,
		EnrollmentID: e.ID,
		OccuredAt:    e.UpdatedAt,
	}
}

func (e *Event) EventName() string { return e.Name.String() }

func (e *Event) Topic() string {
	name := strings.TrimPrefix(e.Name.String(), "enrollment_")
	return "enrollments." + e.EnrollmentID.String() + "." + name
}

type EnrollmentAuthenticatedEvent struct {
	*Event
	Enrollment Enrollment `json:"enrollment"`
}

func NewEnrollmentAuthenticatedEvent(e *Enrollment) events.DomainEvent {
	return &EnrollmentAuthenticatedEvent{
		Event:      NewEvent(EnrollmentAuthenticated, e),
		Enrollment: *e,
	}
}

// EnrollmentTokenUpdatedEvent carries the full snapshot so the durable handler is
// a plain idempotent Store, tolerant of cross-subject reordering.
type EnrollmentTokenUpdatedEvent struct {
	*Event
	Enrollment Enrollment `json:"enrollment"`
	Push       Push       `json:"push"`
}

func NewEnrollmentTokenUpdatedEvent(e *Enrollment) events.DomainEvent {
	return &EnrollmentTokenUpdatedEvent{
		Event:      NewEvent(EnrollmentTokenUpdated, e),
		Enrollment: *e,
		Push:       e.Push,
	}
}

type EnrollmentCheckedOutEvent struct {
	*Event
	Enrollment Enrollment `json:"enrollment"`
}

func NewEnrollmentCheckedOutEvent(e *Enrollment) events.DomainEvent {
	return &EnrollmentCheckedOutEvent{
		Event:      NewEvent(EnrollmentCheckedOut, e),
		Enrollment: *e,
	}
}
