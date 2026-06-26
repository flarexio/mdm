package inmem

import (
	"sync"

	"github.com/flarexio/mdm-server/command"
)

// NewCommandQueue returns an in-memory command.Queue. A scaled deployment swaps in
// a Redis-backed queue behind the same interface (commands must be delivered
// asynchronously, so a shared store is what lets multiple server instances serve
// the same device).
func NewCommandQueue() (command.Queue, error) {
	return &commandQueue{
		queues: make(map[string][]*queuedCommand),
	}, nil
}

// queuedCommand is one command in an enrollment's queue together with its delivery
// state. notNow marks a command the device deferred: it stays queued but is skipped
// within the same connection.
type queuedCommand struct {
	cmd    *command.Command
	notNow bool
}

type commandQueue struct {
	mu     sync.Mutex
	queues map[string][]*queuedCommand
}

func (q *commandQueue) Enqueue(enrollmentID string, cmd *command.Command) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, qc := range q.queues[enrollmentID] {
		if qc.cmd.CommandUUID == cmd.CommandUUID {
			return command.ErrCommandExists
		}
	}

	q.queues[enrollmentID] = append(q.queues[enrollmentID], &queuedCommand{cmd: cmd})
	return nil
}

func (q *commandQueue) Next(enrollmentID string, skipNotNow bool) (*command.Command, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, qc := range q.queues[enrollmentID] {
		if qc.notNow && skipNotNow {
			continue
		}
		return qc.cmd, nil
	}

	return nil, nil
}

func (q *commandQueue) Report(enrollmentID string, result *command.Result) error {
	// Idle is a poll, not a result: nothing in the queue changes.
	if result.Status == command.Idle {
		return nil
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	queue := q.queues[enrollmentID]
	for i, qc := range queue {
		if qc.cmd.CommandUUID != result.CommandUUID {
			continue
		}

		if result.Status.NeedsRetry() {
			// NotNow: keep the command, mark it deferred so it is skipped this
			// connection but retried on the next.
			qc.notNow = true
			return nil
		}

		// Terminal: remove the command from the queue.
		q.queues[enrollmentID] = append(queue[:i], queue[i+1:]...)
		return nil
	}

	// Unknown CommandUUID (e.g. a duplicate report for an already-removed command):
	// tolerate it idempotently rather than erroring.
	return nil
}
