package command

import (
	"time"

	"github.com/flarexio/core/events"
)

// CommandRespondedEvent carries a device's command result as a domain event on the
// event bus, correlated to the original Enqueue by CommandUUID. It is emitted for
// every non-Idle status (including NotNow), decoupled from queue advancement.
type CommandRespondedEvent struct {
	Domain       string         `json:"domain"`
	EnrollmentID string         `json:"enrollment_id"`
	CommandUUID  string         `json:"command_uuid"`
	Status       Status         `json:"status"`
	Result       *CommandResult `json:"result,omitempty"`
	OccuredAt    time.Time      `json:"occured_at"`
}

func (e *CommandRespondedEvent) EventName() string { return "command_responded" }

func (e *CommandRespondedEvent) Topic() string {
	return "commands." + e.EnrollmentID + ".responded"
}

// NewCommandRespondedEvent creates a command_responded event from the typed result.
func NewCommandRespondedEvent(enrollmentID string, cr *CommandResult) events.DomainEvent {
	return &CommandRespondedEvent{
		Domain:       "mdm:commands",
		EnrollmentID: enrollmentID,
		CommandUUID:  cr.CommandUUID,
		Status:       cr.Status,
		Result:       cr,
		OccuredAt:    time.Now(),
	}
}
