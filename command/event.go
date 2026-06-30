package command

import "time"

// RespondedEvent is published when a device reports a command's terminal result.
// It is the asynchronous delivery of the result to interested parties: the
// original enqueue is fire-and-forget, and the result may return on a different
// instance, so consumers correlate back by CommandUUID.
//
// Response is the typed domain model (nil for a command with no typed result, or
// an orphan whose type is unknown); ErrorChain carries the device's error details
// when Status is Error.
type RespondedEvent struct {
	Domain       string       `json:"domain"`
	EnrollmentID string       `json:"enrollment_id"`
	CommandUUID  string       `json:"command_uuid"`
	RequestType  RequestType  `json:"request_type"`
	Status       Status       `json:"status"`
	Response     any          `json:"response,omitempty"`
	ErrorChain   []ErrorChain `json:"error_chain,omitempty"`
	OccurredAt   time.Time    `json:"occurred_at"`
}

func NewRespondedEvent(enrollmentID string, result *Result, requestType RequestType, response any) *RespondedEvent {
	return &RespondedEvent{
		Domain:       "mdm:commands",
		EnrollmentID: enrollmentID,
		CommandUUID:  result.CommandUUID,
		RequestType:  requestType,
		Status:       result.Status,
		Response:     response,
		ErrorChain:   result.ErrorChain,
		OccurredAt:   time.Now(),
	}
}

func (e *RespondedEvent) EventName() string { return "command_responded" }

func (e *RespondedEvent) Topic() string {
	return "commands." + e.EnrollmentID + ".responded"
}
