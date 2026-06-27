package mdm_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm"
	"github.com/flarexio/mdm/checkin"
	"github.com/flarexio/mdm/command"
	"github.com/flarexio/mdm/enrollment"
	"github.com/flarexio/mdm/persistence/inmem"
	"github.com/flarexio/mdm/push"
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

	queue, err := inmem.NewCommandQueue()
	require.NoError(t, err)

	pusher := &fakePusher{}
	return mdm.NewService(repo, queue, pusher), repo, pusher
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

func TestTokenUpdate_RequiresAuthenticateFirst(t *testing.T) {
	svc, _ := newService(t)

	err := svc.TokenUpdate("never-authenticated", &checkin.TokenUpdate{
		Push: checkin.Push{PushMagic: "M", Token: []byte{0x01}},
	})
	assert.ErrorIs(t, err, enrollment.ErrEnrollmentNotFound)
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
