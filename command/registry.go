package command

import "errors"

// ErrUnknownCommand is returned when a RequestType has no registered implementation.
var ErrUnknownCommand = errors.New("unknown command request type")

// CommandDefinition describes an implemented MDM command.
type CommandDefinition struct {
	RequestType RequestType
	Description string
}

// Registry gates command construction: only registered commands can be built and
// invoked. Call sites must use Build rather than constructing raw Commands.
type Registry struct {
	commands map[RequestType]CommandDefinition
}

// NewRegistry returns a Registry pre-populated with every implemented command.
func NewRegistry() *Registry {
	r := &Registry{commands: make(map[RequestType]CommandDefinition)}
	r.Register(CommandDefinition{RequestType: DeviceInformation, Description: "Query device information"})
	r.Register(CommandDefinition{RequestType: InstallProfile, Description: "Install a configuration profile"})
	r.Register(CommandDefinition{RequestType: RemoveProfile, Description: "Remove a configuration profile"})
	r.Register(CommandDefinition{RequestType: DeviceLock, Description: "Lock the device"})
	r.Register(CommandDefinition{RequestType: EraseDevice, Description: "Erase the device"})
	return r
}

// Register adds a command to the registry. It replaces any existing entry for the
// same RequestType.
func (r *Registry) Register(def CommandDefinition) {
	r.commands[def.RequestType] = def
}

// Build constructs a command for the given RequestType if it is registered;
// otherwise it returns ErrUnknownCommand. fields are the type-specific payload.
func (r *Registry) Build(requestType RequestType, fields map[string]any) (*Command, error) {
	if _, ok := r.commands[requestType]; !ok {
		return nil, ErrUnknownCommand
	}
	return Build(requestType, fields)
}

// IsRegistered reports whether a RequestType has a registered implementation.
func (r *Registry) IsRegistered(rt RequestType) bool {
	_, ok := r.commands[rt]
	return ok
}
