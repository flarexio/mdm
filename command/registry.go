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

var registry = map[RequestType]Factory{}

// Register adds a command implementation to the registry, from an init() in the
// file that defines the command. It panics on a duplicate registration.
func Register(rt RequestType, f Factory) {
	if _, exists := registry[rt]; exists {
		panic(fmt.Sprintf("command: RequestType %q already registered", rt))
	}
	registry[rt] = f
}

// NewRequest builds the registered Request for rt from fields, or
// ErrUnsupportedCommand if rt has no implementation.
func NewRequest(rt RequestType, fields map[string]any) (Request, error) {
	f, ok := registry[rt]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedCommand, rt)
	}
	return f(fields)
}

// ResponseDecoder decodes a command's result plist into its typed domain model.
// A device result carries no RequestType, so the decoder is selected by the
// RequestType of the command the result correlates to (by CommandUUID).
type ResponseDecoder interface {
	DecodeResponse(raw []byte) (any, error)
}

var responseDecoders = map[RequestType]ResponseDecoder{}

// RegisterResponse registers the typed-result decoder for a command, from the
// same init() that registers the command. It panics on a duplicate.
func RegisterResponse(rt RequestType, d ResponseDecoder) {
	if _, exists := responseDecoders[rt]; exists {
		panic(fmt.Sprintf("command: response for RequestType %q already registered", rt))
	}
	responseDecoders[rt] = d
}

// DecodeResponse decodes raw into the typed domain model for rt. It returns
// (nil, nil) when rt has no registered response decoder — an acknowledgement with
// no meaningful payload, or an orphan result whose command type is unknown — so
// callers fall back to the generic Result (Status, ErrorChain).
func DecodeResponse(rt RequestType, raw []byte) (any, error) {
	d, ok := responseDecoders[rt]
	if !ok {
		return nil, nil
	}
	return d.DecodeResponse(raw)
}
