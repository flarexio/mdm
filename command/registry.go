package command

import (
	"errors"
	"fmt"
)

// ErrUnsupportedCommand is returned when building a command whose RequestType has
// no registered implementation.
var ErrUnsupportedCommand = errors.New("unsupported command")

// Request is an implemented MDM command. A command can only be built — and so
// enqueued — through a Request, so call sites cannot assemble arbitrary or unknown
// commands from raw fields.
type Request interface {
	// RequestType is the command's on-the-wire type.
	RequestType() RequestType

	// Fields are the type-specific entries placed next to RequestType in the inner
	// Command dict. RequestType itself must not be included.
	Fields() map[string]any
}

// Factory builds a Request from the wire fields an operator supplies (a decoded
// JSON object), validating them in the process.
type Factory func(fields map[string]any) (Request, error)

// ResponseDecoder decodes a command's result plist into its typed domain model.
// A device result carries no RequestType, so the decoder is selected by the
// RequestType of the command the result correlates to (by CommandUUID).
type ResponseDecoder interface {
	DecodeResponse(raw []byte) (any, error)
}

// entry is one command's implementation: how to build it and, optionally, how to
// decode its result.
type entry struct {
	factory  Factory
	response ResponseDecoder // nil when the command has no typed result
}

var registry = map[RequestType]entry{}

// Register adds a command implementation, from an init() in the file that defines
// the command: factory builds the request from wire fields, response decodes its
// result (nil if the command has no typed result). It panics on a duplicate.
func Register(rt RequestType, factory Factory, response ResponseDecoder) {
	if _, exists := registry[rt]; exists {
		panic(fmt.Sprintf("command: RequestType %q already registered", rt))
	}
	registry[rt] = entry{factory, response}
}

// NewRequest builds the registered Request for rt from fields, or
// ErrUnsupportedCommand if rt has no implementation.
func NewRequest(rt RequestType, fields map[string]any) (Request, error) {
	e, ok := registry[rt]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedCommand, rt)
	}
	return e.factory(fields)
}

// DecodeResponse decodes raw into the typed domain model for rt. It returns
// (nil, nil) when rt has no registered response decoder — an acknowledgement with
// no meaningful payload, or an orphan result whose command type is unknown — so
// callers fall back to the generic Result (Status, ErrorChain).
func DecodeResponse(rt RequestType, raw []byte) (any, error) {
	e, ok := registry[rt]
	if !ok || e.response == nil {
		return nil, nil
	}
	return e.response.DecodeResponse(raw)
}
