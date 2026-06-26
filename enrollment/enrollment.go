// Package enrollment is the MDM aggregate: a device's membership in this MDM
// server. Its lifecycle is driven by the check-in messages a device sends —
// Authenticate, TokenUpdate and CheckOut — and tracked as a small state machine:
//
//	(none) --Authenticate--> Pending --TokenUpdate--> Enrolled --CheckOut--> Removed
//
// The watershed is TokenUpdate: only once the server holds the device's APNs push
// credentials can it ever initiate contact. CanPush encodes that invariant.
package enrollment

import (
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/flarexio/core/events"
	"github.com/flarexio/core/model"
)

var ErrEnrollmentNotFound = errors.New("enrollment not found")

// ID identifies an enrollment. It is the MDM enrollment identifier, which matches
// the Common Name of the device's identity certificate — so an mTLS connection can
// be resolved to its enrollment by reading the verified client cert (not the
// device-claimed body).
type ID string

func (id ID) String() string { return string(id) }

// Status is where a device sits in the enrollment lifecycle.
type Status int

const (
	// Pending: Authenticate received and accepted, but no push credentials yet —
	// the server cannot wake this device.
	Pending Status = iota
	// Enrolled: TokenUpdate received; the device is reachable and manageable.
	Enrolled
	// Removed: CheckOut received (best-effort) or reconciled as gone.
	Removed
)

func ParseStatus(status string) (Status, error) {
	switch strings.ToLower(status) {
	case "pending":
		return Pending, nil
	case "enrolled":
		return Enrolled, nil
	case "removed":
		return Removed, nil
	default:
		return -1, errors.New("invalid status")
	}
}

func (s Status) String() string {
	switch s {
	case Pending:
		return "pending"
	case Enrolled:
		return "enrolled"
	case Removed:
		return "removed"
	default:
		return "unknown"
	}
}

// Push holds what the server needs to wake a device through APNs. It is populated
// from a TokenUpdate and consumed by the push module.
type Push struct {
	Topic     string `json:"topic"`
	PushMagic string `json:"push_magic"`
	Token     []byte `json:"token"`
}

// TokenHex returns the APNs device token in the hex form APNs addressing expects.
func (p Push) TokenHex() string {
	return hex.EncodeToString(p.Token)
}

// Enrollment is the aggregate root.
type Enrollment struct {
	ID     ID     `json:"id"`
	UDID   string `json:"udid"`
	Status Status `json:"status"`
	Push   Push   `json:"push"`
	model.Model

	events.EventStore `json:"-"`
}

// NewEnrollment creates a Pending enrollment from an Authenticate check-in.
func NewEnrollment(id ID, udid string) *Enrollment {
	now := time.Now()

	e := &Enrollment{
		ID:     id,
		UDID:   udid,
		Status: Pending,
		Model:  model.Model{CreatedAt: now, UpdatedAt: now},

		EventStore: events.NewEventStore(),
	}

	e.AddEvent(NewEnrollmentAuthenticatedEvent(e))
	return e
}

// UpdateToken applies a TokenUpdate: it records the push credentials and promotes
// the enrollment to Enrolled — the point at which the device becomes reachable.
func (e *Enrollment) UpdateToken(push Push) {
	e.Push = push
	e.Status = Enrolled
	e.UpdatedAt = time.Now()

	e.AddEvent(NewEnrollmentTokenUpdatedEvent(e))
}

// CheckOut applies a CheckOut. This is best-effort on the device's part, so the
// server must not treat the absence of a CheckOut as proof a device is still
// enrolled.
func (e *Enrollment) CheckOut() {
	now := time.Now()
	e.Status = Removed
	e.UpdatedAt = now
	e.DeletedAt = now

	e.AddEvent(NewEnrollmentCheckedOutEvent(e))
}

// CanPush reports whether the server has everything it needs to wake the device via
// APNs. This is the TokenUpdate watershed expressed as behaviour rather than a
// comment: no token, no contact.
func (e *Enrollment) CanPush() bool {
	return e.Status == Enrolled && e.Push.PushMagic != "" && len(e.Push.Token) > 0
}
