package command

import "errors"

var ErrCommandExists = errors.New("command already queued")

// Queue is a per-enrollment command queue implementing the MDM command/report loop.
//
// Commands are delivered FIFO, one at a time, and removed only when the device
// reports a terminal result. The queue is keyed by a plain enrollment ID string so
// it stays decoupled from the enrollment aggregate; an in-memory implementation and
// a Redis-backed one can both sit behind this interface.
type Queue interface {
	// Enqueue appends cmd to the enrollment's queue. Enqueuing a CommandUUID that is
	// already queued for the enrollment returns ErrCommandExists (idempotent).
	Enqueue(enrollmentID string, cmd *Command) error

	// Next returns the next command to deliver, or nil if none is ready.
	//
	// When skipNotNow is true, commands already answered with NotNow in the current
	// connection are skipped, so the server does not re-offer them in a tight loop.
	// On a later poll it is false, so those commands are retried.
	Next(enrollmentID string, skipNotNow bool) (*Command, error)

	// Report records the device's result for a previously delivered command and
	// advances the queue accordingly:
	//   - Idle: a poll only — nothing changes.
	//   - NotNow: the command is kept (marked) for a later retry.
	//   - any terminal status: the command is removed.
	Report(enrollmentID string, result *Result) error

	// Find returns the queued command with the given CommandUUID, or nil if none —
	// used to recover a result's RequestType before Report removes the command.
	Find(enrollmentID string, commandUUID string) (*Command, error)
}
