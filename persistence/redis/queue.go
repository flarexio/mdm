package redis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/flarexio/mdm/command"
)

const (
	queuePrefix   = "mdm:cmdq:"
	queueTTL      = 30 * 24 * time.Hour // safety net for devices that never return
	queueMaxRetry = 50
)

// queuedCommand is one command together with its delivery state. NotNow marks a
// command the device deferred: it stays queued but is skipped within the same
// connection. It mirrors the in-memory queue's element.
type queuedCommand struct {
	Cmd    *command.Command `json:"cmd"`
	NotNow bool             `json:"not_now"`
}

type commandQueue struct {
	rdb *redis.Client
}

// NewCommandQueue dials addr and returns a Redis-backed command.Queue shared by
// every instance. It preserves the in-memory queue's semantics (FIFO, dedup by
// CommandUUID, peek-don't-pop with NotNow deferral) by holding each enrollment's
// queue as a single JSON value mutated under optimistic locking.
func NewCommandQueue(addr, password string, db int) (command.Queue, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &commandQueue{rdb}, nil
}

func queueKey(enrollmentID string) string { return queuePrefix + enrollmentID }

func (q *commandQueue) Enqueue(enrollmentID string, cmd *command.Command) error {
	return q.update(queueKey(enrollmentID), func(queue []*queuedCommand) ([]*queuedCommand, error) {
		for _, qc := range queue {
			if qc.Cmd.CommandUUID == cmd.CommandUUID {
				return nil, command.ErrCommandExists
			}
		}
		return append(queue, &queuedCommand{Cmd: cmd}), nil
	})
}

func (q *commandQueue) Next(enrollmentID string, skipNotNow bool) (*command.Command, error) {
	queue, err := load(context.Background(), q.rdb, queueKey(enrollmentID))
	if err != nil {
		return nil, err
	}

	for _, qc := range queue {
		if qc.NotNow && skipNotNow {
			continue
		}
		return qc.Cmd, nil
	}

	return nil, nil
}

func (q *commandQueue) Report(enrollmentID string, result *command.Result) error {
	// Idle is a poll, not a result: nothing in the queue changes.
	if result.Status == command.Idle {
		return nil
	}

	return q.update(queueKey(enrollmentID), func(queue []*queuedCommand) ([]*queuedCommand, error) {
		for i, qc := range queue {
			if qc.Cmd.CommandUUID != result.CommandUUID {
				continue
			}

			if result.Status.NeedsRetry() {
				// NotNow: keep the command, mark it deferred.
				qc.NotNow = true
				return queue, nil
			}

			// Terminal: remove the command from the queue.
			return append(queue[:i], queue[i+1:]...), nil
		}

		// Unknown CommandUUID (e.g. a duplicate report): tolerate it idempotently.
		return queue, nil
	})
}

// update runs mutate against the enrollment's queue under an optimistic lock,
// retrying on a concurrent write. A mutate error (e.g. ErrCommandExists) is
// returned as-is and is not retried.
func (q *commandQueue) update(key string, mutate func([]*queuedCommand) ([]*queuedCommand, error)) error {
	ctx := context.Background()

	txf := func(tx *redis.Tx) error {
		queue, err := load(ctx, tx, key)
		if err != nil {
			return err
		}

		updated, err := mutate(queue)
		if err != nil {
			return err
		}

		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			if len(updated) == 0 {
				pipe.Del(ctx, key)
				return nil
			}

			data, err := json.Marshal(updated)
			if err != nil {
				return err
			}
			pipe.Set(ctx, key, data, queueTTL)
			return nil
		})
		return err
	}

	for range queueMaxRetry {
		err := q.rdb.Watch(ctx, txf, key)
		if errors.Is(err, redis.TxFailedErr) {
			continue // optimistic-lock conflict: another writer won, retry
		}
		return err
	}

	return errors.New("redis: command queue update exceeded retry limit")
}

func load(ctx context.Context, c redis.Cmdable, key string) ([]*queuedCommand, error) {
	data, err := c.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var queue []*queuedCommand
	if err := json.Unmarshal(data, &queue); err != nil {
		return nil, err
	}
	return queue, nil
}
