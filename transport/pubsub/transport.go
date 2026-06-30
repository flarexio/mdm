// Package pubsub wires enrollment domain events on the bus to the service's
// durable EventHandler. It is the pubsub counterpart of transport/http.
package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flarexio/core/pubsub"

	"github.com/flarexio/mdm"
	"github.com/flarexio/mdm/enrollment"
)

// EventHandler routes an enrollment event to h by the event name in its subject
// (enrollments.<id>.<name>). It is the MessageHandler handed to NATS PullConsume
// or to an in-process Subscribe.
func EventHandler(h mdm.EventHandler) pubsub.MessageHandler {
	return func(_ context.Context, msg *pubsub.Message) error {
		name := msg.Topic[strings.LastIndex(msg.Topic, ".")+1:]

		switch name {
		case "authenticated":
			var e enrollment.EnrollmentAuthenticatedEvent
			if err := json.Unmarshal(msg.Data, &e); err != nil {
				return err
			}
			return h.EnrollmentAuthenticatedHandler(&e)

		case "token_updated":
			var e enrollment.EnrollmentTokenUpdatedEvent
			if err := json.Unmarshal(msg.Data, &e); err != nil {
				return err
			}
			return h.EnrollmentTokenUpdatedHandler(&e)

		case "checked_out":
			var e enrollment.EnrollmentCheckedOutEvent
			if err := json.Unmarshal(msg.Data, &e); err != nil {
				return err
			}
			return h.EnrollmentCheckedOutHandler(&e)

		default:
			return fmt.Errorf("unknown event subject %q", msg.Topic)
		}
	}
}

// RegisterEventHandler subscribes h to every enrollment subject on ps. It suits
// the in-process pubsub (single-node, tests); the NATS path uses EventHandler with
// PullConsume instead.
func RegisterEventHandler(ps pubsub.PubSub, h mdm.EventHandler) error {
	return ps.Subscribe("enrollments.#", EventHandler(h))
}
