// Package mdm is the MDM server's core service: it turns authenticated check-in
// messages into enrollment lifecycle transitions.
package mdm

import (
	"context"
	"errors"

	"github.com/flarexio/mdm-server/checkin"
	"github.com/flarexio/mdm-server/command"
	"github.com/flarexio/mdm-server/enrollment"
	"github.com/flarexio/mdm-server/push"
)

var ErrUnsupportedCheckin = errors.New("unsupported check-in message")

// Service handles both MDM channels. Every method takes the AUTHENTICATED
// enrollment identity (resolved from the mTLS client certificate) separately from
// the message: the identity is the certificate's, never the device-claimed body.
// The message supplies data only.
type Service interface {
	// Check-in channel.
	Authenticate(id enrollment.ID, msg *checkin.Authenticate) error
	TokenUpdate(id enrollment.ID, msg *checkin.TokenUpdate) error
	CheckOut(id enrollment.ID, msg *checkin.CheckOut) error

	// CheckIn dispatches a decoded check-in message to the matching handler.
	CheckIn(id enrollment.ID, msg any) error

	// Command channel.

	// Enqueue queues a command and wakes the device via APNs so it connects and
	// pulls it. If the device cannot be woken the command waits until its next poll.
	Enqueue(id enrollment.ID, cmd *command.Command) error

	// Command handles one turn of the command/report loop: it records the device's
	// result (or Idle poll) and returns the next command to run, or nil if the
	// queue is empty.
	Command(id enrollment.ID, result *command.Result) (*command.Command, error)
}

type ServiceMiddleware func(Service) Service

func NewService(enrollments enrollment.Repository, commands command.Queue, pusher push.Pusher) Service {
	return &service{enrollments, commands, pusher}
}

type service struct {
	enrollments enrollment.Repository
	commands    command.Queue
	pusher      push.Pusher
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

func (svc *service) Enqueue(id enrollment.ID, cmd *command.Command) error {
	if err := svc.commands.Enqueue(string(id), cmd); err != nil {
		return err
	}

	return svc.wake(id)
}

// wake sends an APNs push so the device connects and pulls the queued command.
//
// It is tolerant: a device that is unknown or not yet pushable is skipped without
// error — the command simply waits in the queue until the device next polls. A push
// that reports the token is dead (Unregistered) reconciles the enrollment to
// Removed, recovering the state a missing CheckOut would have left stale.
func (svc *service) wake(id enrollment.ID) error {
	e, err := svc.enrollments.Find(id)
	if err != nil {
		if errors.Is(err, enrollment.ErrEnrollmentNotFound) {
			return nil
		}
		return err
	}

	if !e.CanPush() {
		return nil
	}

	err = svc.pusher.Push(context.Background(), push.Target{
		Token:     e.Push.TokenHex(),
		Topic:     e.Push.Topic,
		PushMagic: e.Push.PushMagic,
	})
	if err == nil {
		return nil
	}

	var apnsErr *push.Error
	if errors.As(err, &apnsErr) && apnsErr.Unregistered() {
		e.CheckOut()
		return svc.enrollments.Store(e)
	}

	return err
}

// Command runs one turn of the command/report loop. It records the incoming result
// (Idle is a no-op in the queue) and returns the next command. skipNotNow is set
// when the device just deferred a command, so the server does not re-offer it in
// the same connection — it will be retried on the next poll.
func (svc *service) Command(id enrollment.ID, result *command.Result) (*command.Command, error) {
	if err := svc.commands.Report(string(id), result); err != nil {
		return nil, err
	}

	return svc.commands.Next(string(id), result.Status == command.NotNow)
}
