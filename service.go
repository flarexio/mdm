// Package mdm is the MDM server's core service: it turns authenticated check-in
// messages into enrollment lifecycle transitions.
package mdm

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/flarexio/core/pubsub"
	"github.com/flarexio/mdm/checkin"
	"github.com/flarexio/mdm/command"
	"github.com/flarexio/mdm/enrollment"
	"github.com/flarexio/mdm/push"
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

	// Query.

	// Enrollments returns every enrollment the server knows about.
	Enrollments() ([]*enrollment.Enrollment, error)

	// Enrollment returns a single enrollment by its ID.
	Enrollment(id enrollment.ID) (*enrollment.Enrollment, error)

	// Handler returns the durable projector that applies enrollment events to the
	// Repository; the pubsub transport subscribes it (see RegisterEventHandler).
	Handler() (EventHandler, error)
}

// EventHandler applies enrollment events to the durable Repository: the eventual
// half of the flow whose strong half is the Cache write-through. Each event
// carries the full snapshot, so every handler is an idempotent Store.
type EventHandler interface {
	EnrollmentAuthenticatedHandler(e *enrollment.EnrollmentAuthenticatedEvent) error
	EnrollmentTokenUpdatedHandler(e *enrollment.EnrollmentTokenUpdatedEvent) error
	EnrollmentCheckedOutHandler(e *enrollment.EnrollmentCheckedOutEvent) error
}

type ServiceMiddleware func(Service) Service

// DefaultPendingTTL bounds an enrollment's life in the Cache: long enough to
// outlast the lag before the durable Store catches up.
const DefaultPendingTTL = 1 * time.Hour

func NewService(
	enrollments enrollment.Repository,
	cache enrollment.Cache,
	commands command.Queue,
	pusher push.Pusher,
) Service {
	return &service{enrollments, cache, commands, pusher, DefaultPendingTTL}
}

type service struct {
	enrollments enrollment.Repository // durable SoR, written by the event handler (eventual)
	cache       enrollment.Cache      // shared strong-consistency bridge (Redis)
	commands    command.Queue
	pusher      push.Pusher
	pendingTTL  time.Duration
}

// Authenticate begins an enrollment. It writes through to the Cache so the
// immediately-following first TokenUpdate reads it back before the event-driven
// durable Store catches up.
func (svc *service) Authenticate(id enrollment.ID, msg *checkin.Authenticate) error {
	e := enrollment.NewEnrollment(id, msg.UDID)

	if err := svc.cache.Store(e, svc.pendingTTL); err != nil {
		return err
	}

	return e.Notify()
}

// TokenUpdate stores the device's APNs credentials and promotes it to Enrolled.
// It reads prior state from the durable store, then the Cache, and upserts if
// neither has it: Apple may never resend a TokenUpdate, and the mTLS certificate
// has already authenticated the device.
func (svc *service) TokenUpdate(id enrollment.ID, msg *checkin.TokenUpdate) error {
	e, err := svc.enrollments.Find(id)
	if errors.Is(err, enrollment.ErrEnrollmentNotFound) {
		e, err = svc.cache.Find(id)
	}

	switch {
	case errors.Is(err, enrollment.ErrEnrollmentNotFound):
		e = enrollment.NewEnrollment(id, msg.UDID) // upsert
	case err != nil:
		return err
	}

	e.UpdateToken(enrollment.Push{
		Topic:     msg.Topic,
		PushMagic: msg.PushMagic,
		Token:     msg.Token,
	})

	return e.Notify()
}

// CheckOut marks the enrollment Removed. Best-effort: an unknown enrollment is no
// error. It reads the durable store only — teardown is outside the Cache's window.
func (svc *service) CheckOut(id enrollment.ID, _ *checkin.CheckOut) error {
	e, err := svc.enrollments.Find(id)
	if err != nil {
		if errors.Is(err, enrollment.ErrEnrollmentNotFound) {
			return nil
		}
		return err
	}

	e.CheckOut()
	return e.Notify()
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

// wake pushes the device so it pulls the queued command. It reads the durable
// store only (the command channel deals with enrolled devices). Tolerant: an
// unknown or not-yet-pushable device is skipped and the command waits for the
// next poll. A dead token (Unregistered) reconciles the enrollment to Removed.
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
		return e.Notify()
	}

	return err
}

// Command runs one turn of the command/report loop. It records the incoming
// result (Idle is a no-op in the queue) and returns the next command. skipNotNow
// is set when the device just deferred a command, so the server does not re-offer
// it in the same connection — it will be retried on the next poll.
func (svc *service) Command(id enrollment.ID, result *command.Result) (*command.Command, error) {
	if err := svc.commands.Report(string(id), result); err != nil {
		return nil, err
	}

	return svc.commands.Next(string(id), result.Status == command.NotNow)
}

func (svc *service) Enrollments() ([]*enrollment.Enrollment, error) {
	return svc.enrollments.ListAll()
}

func (svc *service) Enrollment(id enrollment.ID) (*enrollment.Enrollment, error) {
	return svc.enrollments.Find(id)
}

// --- Event sourcing: the durable projection ---

func (svc *service) Handler() (EventHandler, error) { return svc, nil }

func (svc *service) EnrollmentAuthenticatedHandler(e *enrollment.EnrollmentAuthenticatedEvent) error {
	return svc.enrollments.Store(&e.Enrollment)
}

func (svc *service) EnrollmentTokenUpdatedHandler(e *enrollment.EnrollmentTokenUpdatedEvent) error {
	return svc.enrollments.Store(&e.Enrollment)
}

func (svc *service) EnrollmentCheckedOutHandler(e *enrollment.EnrollmentCheckedOutEvent) error {
	// Soft delete: keep the Removed record (the snapshot has Status=Removed and
	// DeletedAt set) so a departed device stays queryable.
	return svc.enrollments.Store(&e.Enrollment)
}

// RegisterEventHandler subscribes h to the enrollment event subjects on ps, so a
// Notify() from any instance is applied to that instance's durable Repository.
// Topics mirror enrollment.Event.Topic(): enrollments.<id>.<name>.
func RegisterEventHandler(ps pubsub.PubSub, h EventHandler) error {
	subs := map[string]func([]byte) error{
		"enrollments.*.authenticated": func(b []byte) error {
			var e enrollment.EnrollmentAuthenticatedEvent
			if err := json.Unmarshal(b, &e); err != nil {
				return err
			}
			return h.EnrollmentAuthenticatedHandler(&e)
		},
		"enrollments.*.token_updated": func(b []byte) error {
			var e enrollment.EnrollmentTokenUpdatedEvent
			if err := json.Unmarshal(b, &e); err != nil {
				return err
			}
			return h.EnrollmentTokenUpdatedHandler(&e)
		},
		"enrollments.*.checked_out": func(b []byte) error {
			var e enrollment.EnrollmentCheckedOutEvent
			if err := json.Unmarshal(b, &e); err != nil {
				return err
			}
			return h.EnrollmentCheckedOutHandler(&e)
		},
	}

	for topic, apply := range subs {
		if err := ps.Subscribe(topic, func(_ context.Context, msg *pubsub.Message) error {
			return apply(msg.Data)
		}); err != nil {
			return err
		}
	}

	return nil
}
