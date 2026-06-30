package mdm_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/core/events"
	"github.com/flarexio/core/pubsub"
	"github.com/flarexio/mdm"
	"github.com/flarexio/mdm/checkin"
	"github.com/flarexio/mdm/command"
	"github.com/flarexio/mdm/enrollment"
	"github.com/flarexio/mdm/persistence/inmem"
	"github.com/flarexio/mdm/push"

	transpubsub "github.com/flarexio/mdm/transport/pubsub"
)

// fakePusher records the pushes the service makes and can be made to fail.
type fakePusher struct {
	pushed []push.Target
	err    error
}

func (f *fakePusher) Push(_ context.Context, t push.Target) error {
	f.pushed = append(f.pushed, t)
	return f.err
}

func newService(t *testing.T) (mdm.Service, enrollment.Repository) {
	svc, repo, _ := newServiceWithPush(t)
	return svc, repo
}

func newServiceWithPush(t *testing.T) (mdm.Service, enrollment.Repository, *fakePusher) {
	t.Helper()

	repo, err := inmem.NewEnrollmentRepository()
	require.NoError(t, err)

	cache, err := inmem.NewEnrollmentCache()
	require.NoError(t, err)

	queue, err := inmem.NewCommandQueue()
	require.NoError(t, err)

	// Synchronous pubsub: the durable Store happens in the subscribed event
	// handler, so Notify() is read-your-write and assertions on repo see the state
	// immediately after a check-in call.
	ps := inmem.NewPubSub()
	events.ReplaceGlobals(ps)

	pusher := &fakePusher{}
	svc := mdm.NewService(repo, cache, queue, pusher)

	handler, err := svc.Handler()
	require.NoError(t, err)
	require.NoError(t, transpubsub.RegisterEventHandler(ps, handler))

	return svc, repo, pusher
}

// enroll runs a device through Authenticate + TokenUpdate so it is pushable.
func enroll(t *testing.T, svc mdm.Service, id enrollment.ID) {
	t.Helper()
	require.NoError(t, svc.Authenticate(id, &checkin.Authenticate{
		Enrollment: checkin.Enrollment{UDID: "UDID-" + string(id)},
	}))
	require.NoError(t, svc.TokenUpdate(id, &checkin.TokenUpdate{
		Push: checkin.Push{
			Topic:     "com.apple.mgmt.External.abc",
			PushMagic: "MAGIC-123",
			Token:     []byte{0x01, 0x02, 0x03, 0x04},
		},
	}))
}

func TestCheckInLifecycle(t *testing.T) {
	svc, repo := newService(t)
	const id enrollment.ID = "device-0001"

	require.NoError(t, svc.Authenticate(id, &checkin.Authenticate{
		Enrollment: checkin.Enrollment{UDID: "UDID-0001"},
	}))

	t.Run("authenticate stores a pending enrollment", func(t *testing.T) {
		e, err := repo.Find(id)
		require.NoError(t, err)
		assert.Equal(t, enrollment.Pending, e.Status)
		assert.False(t, e.CanPush())
	})

	require.NoError(t, svc.TokenUpdate(id, &checkin.TokenUpdate{
		Push: checkin.Push{
			Topic:     "com.apple.mgmt.External.abc",
			PushMagic: "MAGIC-123",
			Token:     []byte{0x01, 0x02, 0x03, 0x04},
		},
	}))

	t.Run("token update makes it enrolled and pushable", func(t *testing.T) {
		e, err := repo.Find(id)
		require.NoError(t, err)
		assert.Equal(t, enrollment.Enrolled, e.Status)
		assert.True(t, e.CanPush())
	})

	require.NoError(t, svc.CheckOut(id, &checkin.CheckOut{}))

	t.Run("check out marks it removed", func(t *testing.T) {
		e, err := repo.Find(id)
		require.NoError(t, err)
		assert.Equal(t, enrollment.Removed, e.Status)
	})
}

// TestIdentityComesFromCertNotBody is the crux: the enrollment is keyed by the
// authenticated identity (the cert CN passed as id), not by the UDID the device
// claims in the body.
func TestIdentityComesFromCertNotBody(t *testing.T) {
	svc, repo := newService(t)

	const certIdentity enrollment.ID = "device-from-cert"

	require.NoError(t, svc.Authenticate(certIdentity, &checkin.Authenticate{
		Enrollment: checkin.Enrollment{UDID: "udid-the-device-claims"},
	}))

	// Stored under the cert identity ...
	e, err := repo.Find(certIdentity)
	require.NoError(t, err)
	assert.Equal(t, certIdentity, e.ID)
	assert.Equal(t, "udid-the-device-claims", e.UDID, "body UDID is kept as data only")
}

// TestTokenUpdate_UpsertsWithoutAuthenticate covers the orphan TokenUpdate: with
// neither a durable record nor a cache entry, the service upserts from the
// message rather than rejecting. Apple does not guarantee the device will resend
// a TokenUpdate, and the mTLS certificate has already authenticated it.
func TestTokenUpdate_UpsertsWithoutAuthenticate(t *testing.T) {
	svc, repo := newService(t)

	const id enrollment.ID = "never-authenticated"
	require.NoError(t, svc.TokenUpdate(id, &checkin.TokenUpdate{
		Enrollment: checkin.Enrollment{UDID: "UDID-orphan"},
		Push:       checkin.Push{Topic: "com.apple.mgmt.External.x", PushMagic: "M", Token: []byte{0x01}},
	}))

	e, err := repo.Find(id)
	require.NoError(t, err)
	assert.Equal(t, enrollment.Enrolled, e.Status)
	assert.True(t, e.CanPush())
}

func TestCheckOut_UnknownIsNoError(t *testing.T) {
	svc, _ := newService(t)

	// Best-effort: nothing to remove, not an error.
	assert.NoError(t, svc.CheckOut("never-seen", &checkin.CheckOut{}))
}

func TestCheckIn_Dispatch(t *testing.T) {
	svc, repo := newService(t)
	const id enrollment.ID = "device-0001"

	var msg any = &checkin.Authenticate{Enrollment: checkin.Enrollment{UDID: "UDID-0001"}}
	require.NoError(t, svc.CheckIn(id, msg))

	_, err := repo.Find(id)
	require.NoError(t, err)

	// An unmodelled message type is rejected.
	assert.ErrorIs(t, svc.CheckIn(id, "not a check-in"), mdm.ErrUnsupportedCheckin)
}

func TestEnqueueWakesPushableDevice(t *testing.T) {
	svc, _, pusher := newServiceWithPush(t)
	const id enrollment.ID = "device-0001"
	enroll(t, svc, id)

	require.NoError(t, svc.Enqueue(id, &command.Command{CommandUUID: "cmd-1"}))

	require.Len(t, pusher.pushed, 1, "enqueue wakes the device")
	assert.Equal(t, "01020304", pusher.pushed[0].Token)
	assert.Equal(t, "com.apple.mgmt.External.abc", pusher.pushed[0].Topic)
	assert.Equal(t, "MAGIC-123", pusher.pushed[0].PushMagic)
}

func TestEnqueueSkipsPushWhenNotPushable(t *testing.T) {
	svc, _, pusher := newServiceWithPush(t)
	const id enrollment.ID = "device-0001"

	// Authenticated but no TokenUpdate yet: not reachable.
	require.NoError(t, svc.Authenticate(id, &checkin.Authenticate{}))
	require.NoError(t, svc.Enqueue(id, &command.Command{CommandUUID: "cmd-1"}))

	assert.Empty(t, pusher.pushed, "a pending device cannot be pushed; the command waits")
}

func TestEnqueueReconcilesOnUnregistered(t *testing.T) {
	svc, repo, pusher := newServiceWithPush(t)
	pusher.err = &push.Error{Status: 410, Reason: "Unregistered"}

	const id enrollment.ID = "device-0001"
	enroll(t, svc, id)

	require.NoError(t, svc.Enqueue(id, &command.Command{CommandUUID: "cmd-1"}))

	e, err := repo.Find(id)
	require.NoError(t, err)
	assert.Equal(t, enrollment.Removed, e.Status, "a dead token reconciles the enrollment to Removed")
}

// commandEventRecorder subscribes to command_responded events and captures them.
type commandEventRecorder struct {
	events []*command.CommandRespondedEvent
}

func (r *commandEventRecorder) handler() func(ctx context.Context, msg *pubsub.Message) error {
	return func(_ context.Context, msg *pubsub.Message) error {
		var e command.CommandRespondedEvent
		if err := json.Unmarshal(msg.Data, &e); err != nil {
			return err
		}
		r.events = append(r.events, &e)
		return nil
	}
}

func TestCommand_TerminalEmitsEvent(t *testing.T) {
	svc, _, _ := newServiceWithPush(t)
	const id enrollment.ID = "device-0001"

	// Subscribe to command_responded before the call.
	recorder := &commandEventRecorder{}
	ps := inmemPubSub(t)
	require.NoError(t, ps.Subscribe("commands.#", recorder.handler()))

	require.NoError(t, svc.Enqueue(id, &command.Command{CommandUUID: "cmd-1"}))
	_, err := svc.Command(id, &command.Result{CommandUUID: "cmd-1", Status: command.Acknowledged})
	require.NoError(t, err)

	require.Len(t, recorder.events, 1)
	assert.Equal(t, "cmd-1", recorder.events[0].CommandUUID)
	assert.Equal(t, command.Acknowledged, recorder.events[0].Status)
}

func TestCommand_NotNowEmitsEvent(t *testing.T) {
	svc, _, _ := newServiceWithPush(t)
	const id enrollment.ID = "device-0001"

	recorder := &commandEventRecorder{}
	ps := inmemPubSub(t)
	require.NoError(t, ps.Subscribe("commands.#", recorder.handler()))

	require.NoError(t, svc.Enqueue(id, &command.Command{CommandUUID: "cmd-nn"}))
	_, err := svc.Command(id, &command.Result{CommandUUID: "cmd-nn", Status: command.NotNow})
	require.NoError(t, err)

	require.Len(t, recorder.events, 1)
	assert.Equal(t, command.NotNow, recorder.events[0].Status)
}

func TestCommand_IdleDoesNotEmitEvent(t *testing.T) {
	svc, _, _ := newServiceWithPush(t)
	const id enrollment.ID = "device-0001"

	recorder := &commandEventRecorder{}
	ps := inmemPubSub(t)
	require.NoError(t, ps.Subscribe("commands.#", recorder.handler()))

	_, err := svc.Command(id, &command.Result{Status: command.Idle})
	require.NoError(t, err)

	assert.Empty(t, recorder.events, "Idle poll must not emit command_responded")
}

func TestCommand_EventEmittedOnUnknownUUID(t *testing.T) {
	svc, _, _ := newServiceWithPush(t)
	const id enrollment.ID = "device-0001"

	recorder := &commandEventRecorder{}
	ps := inmemPubSub(t)
	require.NoError(t, ps.Subscribe("commands.#", recorder.handler()))

	// Report a UUID that was never enqueued: queue advancement is a no-op,
	// but the event is still emitted so consumers get the result content.
	_, err := svc.Command(id, &command.Result{CommandUUID: "unknown-cmd", Status: command.Acknowledged})
	require.NoError(t, err)

	require.Len(t, recorder.events, 1)
	assert.Equal(t, "unknown-cmd", recorder.events[0].CommandUUID)
}

func TestCommand_EventIncludesTypedResultPayload(t *testing.T) {
	svc, _, _ := newServiceWithPush(t)
	const id enrollment.ID = "device-0001"

	recorder := &commandEventRecorder{}
	ps := inmemPubSub(t)
	require.NoError(t, ps.Subscribe("commands.#", recorder.handler()))

	require.NoError(t, svc.Enqueue(id, &command.Command{CommandUUID: "cmd-info"}))

	// Simulate a DeviceInformation result with QueryResponses.
	result, err := command.DecodeResult([]byte(`<plist version="1.0"><dict>
		<key>CommandUUID</key><string>cmd-info</string>
		<key>Status</key><string>Acknowledged</string>
		<key>QueryResponses</key>
		<dict><key>DeviceName</key><string>TestiPhone</string></dict>
	</dict></plist>`))
	require.NoError(t, err)

	_, err = svc.Command(id, result)
	require.NoError(t, err)

	require.Len(t, recorder.events, 1)
	require.NotNil(t, recorder.events[0].Result)
	require.NotNil(t, recorder.events[0].Result.Payload)

	qr, ok := recorder.events[0].Result.Payload["QueryResponses"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "TestiPhone", qr["DeviceName"])
}

func TestCommand_EventTopic(t *testing.T) {
	svc, _, _ := newServiceWithPush(t)
	const id enrollment.ID = "device-0001"

	recorder := &commandEventRecorder{}
	ps := inmemPubSub(t)
	require.NoError(t, ps.Subscribe("commands.#", recorder.handler()))

	require.NoError(t, svc.Enqueue(id, &command.Command{CommandUUID: "cmd-1"}))
	_, err := svc.Command(id, &command.Result{CommandUUID: "cmd-1", Status: command.Acknowledged})
	require.NoError(t, err)

	require.Len(t, recorder.events, 1)
	// The event carries the enrollment ID for correlation.
	assert.Equal(t, string(id), recorder.events[0].EnrollmentID)
}

// inmemPubSub returns the in-memory pubsub currently installed by ReplaceGlobals.
// The caller must have set it up via events.ReplaceGlobals first (which newServiceWithPush does).
func inmemPubSub(t *testing.T) *inmemPubSubWrapper {
	t.Helper()
	// The in-memory pubsub was set via events.ReplaceGlobals in newServiceWithPush.
	// We can't access the global instance directly, but we can re-create one and
	// swap it back after the test. Instead, subscribe via the global-pubsub path:
	// use events.NewEventStore().Notify() to publish, and a separate subscriber.
	//
	// Since the service publishes via events.NewEventStore().Notify() which uses
	// the global pubsub, we need to subscribe on the SAME pubsub instance.
	// Return a fresh pubsub and re-install it: the service constructor already
	// ran, so any new pubsub we install now intercepts future publishes.
	ps := inmem.NewPubSub()
	events.ReplaceGlobals(ps)
	return &inmemPubSubWrapper{ps}
}

type inmemPubSubWrapper struct {
	pubsub.PubSub
}

