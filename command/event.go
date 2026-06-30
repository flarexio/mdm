package command

import "time"

// RespondedEvent delivers a command's terminal result to interested parties, who
// correlate it back to the enqueued command by CommandUUID. Response is the typed
// domain model (nil if none); ErrorChain is set when Status is Error.
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
