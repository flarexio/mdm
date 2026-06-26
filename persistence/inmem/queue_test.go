package inmem_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm-server/command"
	"github.com/flarexio/mdm-server/persistence/inmem"
)

func cmd(uuid string) *command.Command {
	c := &command.Command{CommandUUID: uuid}
	c.Command.RequestType = command.DeviceInformation
	return c
}

const dev = "device-0001"

func TestQueue_FIFOOneAtATime(t *testing.T) {
	q, err := inmem.NewCommandQueue()
	require.NoError(t, err)

	require.NoError(t, q.Enqueue(dev, cmd("cmd-1")))
	require.NoError(t, q.Enqueue(dev, cmd("cmd-2")))

	// One at a time, in order — and Next does not advance on its own.
	next, err := q.Next(dev, false)
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.Equal(t, "cmd-1", next.CommandUUID)

	again, _ := q.Next(dev, false)
	assert.Equal(t, "cmd-1", again.CommandUUID, "without a report the head does not move")
}

func TestQueue_TerminalRemovesAndAdvances(t *testing.T) {
	q, _ := inmem.NewCommandQueue()
	require.NoError(t, q.Enqueue(dev, cmd("cmd-1")))
	require.NoError(t, q.Enqueue(dev, cmd("cmd-2")))

	require.NoError(t, q.Report(dev, &command.Result{CommandUUID: "cmd-1", Status: command.Acknowledged}))

	next, _ := q.Next(dev, false)
	require.NotNil(t, next)
	assert.Equal(t, "cmd-2", next.CommandUUID, "acknowledged command is removed")
}

func TestQueue_NotNowKeptSkippedThenRetried(t *testing.T) {
	q, _ := inmem.NewCommandQueue()
	require.NoError(t, q.Enqueue(dev, cmd("cmd-1")))
	require.NoError(t, q.Enqueue(dev, cmd("cmd-2")))

	// Device defers cmd-1 with NotNow.
	require.NoError(t, q.Report(dev, &command.Result{CommandUUID: "cmd-1", Status: command.NotNow}))

	t.Run("skipped within the same connection", func(t *testing.T) {
		next, _ := q.Next(dev, true) // skipNotNow
		require.NotNil(t, next)
		assert.Equal(t, "cmd-2", next.CommandUUID)
	})

	t.Run("retried on a later poll", func(t *testing.T) {
		next, _ := q.Next(dev, false) // a fresh connection
		require.NotNil(t, next)
		assert.Equal(t, "cmd-1", next.CommandUUID, "NotNow command comes back")
	})

	t.Run("acknowledging it later removes it", func(t *testing.T) {
		require.NoError(t, q.Report(dev, &command.Result{CommandUUID: "cmd-1", Status: command.Acknowledged}))
		next, _ := q.Next(dev, false)
		require.NotNil(t, next)
		assert.Equal(t, "cmd-2", next.CommandUUID)
	})
}

func TestQueue_IdleChangesNothing(t *testing.T) {
	q, _ := inmem.NewCommandQueue()
	require.NoError(t, q.Enqueue(dev, cmd("cmd-1")))

	require.NoError(t, q.Report(dev, &command.Result{Status: command.Idle}))

	next, _ := q.Next(dev, false)
	require.NotNil(t, next)
	assert.Equal(t, "cmd-1", next.CommandUUID, "idle poll must not consume a command")
}

func TestQueue_EnqueueDedup(t *testing.T) {
	q, _ := inmem.NewCommandQueue()
	require.NoError(t, q.Enqueue(dev, cmd("cmd-1")))
	assert.ErrorIs(t, q.Enqueue(dev, cmd("cmd-1")), command.ErrCommandExists)
}

func TestQueue_EmptyReturnsNil(t *testing.T) {
	q, _ := inmem.NewCommandQueue()
	next, err := q.Next(dev, false)
	require.NoError(t, err)
	assert.Nil(t, next)
}
