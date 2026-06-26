// Package mdm is the MDM server's core service: it turns authenticated check-in
// messages into enrollment lifecycle transitions.
package mdm

import (
	"errors"

	"github.com/flarexio/mdm-server/checkin"
	"github.com/flarexio/mdm-server/enrollment"
)

var ErrUnsupportedCheckin = errors.New("unsupported check-in message")

// Service handles the MDM check-in channel. Every method takes the AUTHENTICATED
// enrollment identity (resolved from the mTLS client certificate) separately from
// the message: the identity is the certificate's, never the device-claimed body.
// The message supplies data only.
type Service interface {
	Authenticate(id enrollment.ID, msg *checkin.Authenticate) error
	TokenUpdate(id enrollment.ID, msg *checkin.TokenUpdate) error
	CheckOut(id enrollment.ID, msg *checkin.CheckOut) error

	// CheckIn dispatches a decoded check-in message to the matching handler.
	CheckIn(id enrollment.ID, msg any) error
}

type ServiceMiddleware func(Service) Service

func NewService(enrollments enrollment.Repository) Service {
	return &service{enrollments}
}

type service struct {
	enrollments enrollment.Repository
}

// Authenticate begins (or restarts) an enrollment. The identity is the cert's; the
// message contributes only data such as the UDID.
//
// Persistence is synchronous here. In the full flarexio event-sourced deployment
// the aggregate would be Notify()'d to the event bus and an EventHandler would do
// the Store — that pubsub wiring is deferred to the module that adds NATS.
func (svc *service) Authenticate(id enrollment.ID, msg *checkin.Authenticate) error {
	e := enrollment.NewEnrollment(id, msg.UDID)
	return svc.enrollments.Store(e)
}

// TokenUpdate records the device's APNs push credentials and promotes it to
// Enrolled. It requires a prior Authenticate: the strict state machine makes the
// lifecycle explicit. (A production server may instead upsert here for resilience.)
func (svc *service) TokenUpdate(id enrollment.ID, msg *checkin.TokenUpdate) error {
	e, err := svc.enrollments.Find(id)
	if err != nil {
		return err
	}

	e.UpdateToken(enrollment.Push{
		Topic:     msg.Topic,
		PushMagic: msg.PushMagic,
		Token:     msg.Token,
	})

	return svc.enrollments.Store(e)
}

// CheckOut marks the enrollment Removed. It is best-effort on the device's side, so
// an unknown enrollment is not an error: there is simply nothing to remove.
func (svc *service) CheckOut(id enrollment.ID, _ *checkin.CheckOut) error {
	e, err := svc.enrollments.Find(id)
	if err != nil {
		if errors.Is(err, enrollment.ErrEnrollmentNotFound) {
			return nil
		}
		return err
	}

	e.CheckOut()

	// Keep the record as Removed (soft delete) rather than dropping it, so the
	// history of a device that left remains queryable.
	return svc.enrollments.Store(e)
}

func (svc *service) CheckIn(id enrollment.ID, msg any) error {
	switch m := msg.(type) {
	case *checkin.Authenticate:
		return svc.Authenticate(id, m)
	case *checkin.TokenUpdate:
		return svc.TokenUpdate(id, m)
	case *checkin.CheckOut:
		return svc.CheckOut(id, m)
	default:
		return ErrUnsupportedCheckin
	}
}
