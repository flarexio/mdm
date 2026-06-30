// Package pubsub wires enrollment domain events on the bus to the service's
// durable EventHandler. It is the pubsub counterpart of transport/http.
package pubsub

import (
	"context"
	"encoding/json"

	"github.com/flarexio/core/pubsub"

	"github.com/flarexio/mdm"
	"github.com/flarexio/mdm/enrollment"
)

// RegisterEventHandler subscribes h to the enrollment event subjects on ps, so a
// Notify() from any instance is applied to that instance's durable Repository.
// Topics mirror enrollment.Event.Topic(): enrollments.<id>.<name>.
func RegisterEventHandler(ps pubsub.PubSub, h mdm.EventHandler) error {
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
