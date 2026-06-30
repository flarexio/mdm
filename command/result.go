package command

import (
	"errors"

	"github.com/micromdm/plist"
)

var ErrInvalidResult = errors.New("invalid result: missing Status")

// Status is the device's response to a delivered command, or its poll state.
//
// It is modelled as a string (not an int enum) on purpose: these exact strings
// are the on-the-wire protocol contract sent by Apple devices, so the type should
// match the wire format directly rather than translate through an internal enum.
type Status string

const (
	// Idle is not a command result but a poll: "I have nothing to report, do you
	// have any work for me?" It carries no CommandUUID.
	Idle Status = "Idle"

	// Acknowledged: the command executed successfully.
	Acknowledged Status = "Acknowledged"

	// Error: the command failed; details are in ErrorChain.
	Error Status = "Error"

	// CommandFormatError: the command plist itself was malformed.
	CommandFormatError Status = "CommandFormatError"

	// NotNow: the device received the command but cannot act on it right now
	// (most commonly because it is locked). It is NOT a failure or a refusal.
	NotNow Status = "NotNow"
)

// NeedsRetry reports whether the command should be kept and retried later.
//
// Only NotNow means "received but deferred": the command must stay queued and be
// re-offered on a later connection (e.g. after the user unlocks). Within the same
// connection the server should stop re-offering a NotNow'd command to avoid a tight
// loop — but it must never be discarded as if completed.
func (s Status) NeedsRetry() bool {
	return s == NotNow
}

// IsTerminal reports whether the command reached a final state and the queue may
// advance past it. NotNow and Idle are deliberately not terminal.
func (s Status) IsTerminal() bool {
	switch s {
	case Acknowledged, Error, CommandFormatError:
		return true
	default:
		return false
	}
}

// ErrorChain carries the error details a device reports when Status == Error.
// Apple sends an array because failures can cascade through several layers.
type ErrorChain struct {
	ErrorCode            int
	ErrorDomain          string
	LocalizedDescription string
	USEnglishDescription string `plist:",omitempty"`
}

// Result represents a device's "command and report results" request: the outcome
// of a previously delivered command, correlated back by CommandUUID. An Idle poll
// is also a Result, but with Status == Idle and an empty CommandUUID.
type Result struct {
	CommandUUID string       `plist:",omitempty"` // empty for an Idle poll
	Status      Status       //
	ErrorChain  []ErrorChain `plist:",omitempty"`
	Raw         []byte       `plist:"-"`
}

// DecodeResult parses a raw result plist. Status is the one field that must always
// be present; CommandUUID is absent on an Idle poll, so it is not required here.
func DecodeResult(raw []byte) (*Result, error) {
	if len(raw) == 0 {
		return nil, ErrEmptyCommand
	}

	var r Result
	if err := plist.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	r.Raw = raw

	if r.Status == "" {
		return nil, ErrInvalidResult
	}

	return &r, nil
}

// CommandResult is the typed domain model for a device's command response. It holds
// the decoded, type-specific payload extracted from the result plist — no raw plist
// bytes travel on the event bus.
type CommandResult struct {
	CommandUUID string         `json:"command_uuid"`
	Status      Status         `json:"status"`
	ErrorChain  []ErrorChain   `json:"error_chain,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
}

// DecodeCommandResult converts a wire-format Result into a typed CommandResult by
// stripting the known envelope fields (Status, CommandUUID, UDID) and keeping the
// remaining type-specific payload. Best-effort: a parse failure still returns a
// populated CommandResult without Payload.
func DecodeCommandResult(result *Result) (*CommandResult, error) {
	cr := &CommandResult{
		CommandUUID: result.CommandUUID,
		Status:      result.Status,
		ErrorChain:  result.ErrorChain,
	}

	if len(result.Raw) > 0 {
		var payload map[string]any
		if err := plist.Unmarshal(result.Raw, &payload); err != nil {
			return cr, nil
		}
		delete(payload, "Status")
		delete(payload, "CommandUUID")
		delete(payload, "UDID")
		if len(payload) > 0 {
			cr.Payload = payload
		}
	}

	return cr, nil
}
