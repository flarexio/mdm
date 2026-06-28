package command

import (
	"github.com/google/uuid"
	"github.com/micromdm/plist"
)

// Build assembles a deliverable command from a RequestType and its type-specific
// fields, assigning a fresh CommandUUID. fields are the payload that sits next to
// RequestType inside the inner Command dict (e.g. Queries for DeviceInformation);
// RequestType is set automatically and cannot be overridden.
//
// The marshalled plist is kept in Raw, exactly as a decoded command would be, so
// the queue and command channel treat built and device-bound commands alike.
func Build(requestType RequestType, fields map[string]any) (*Command, error) {
	if requestType == "" {
		return nil, ErrInvalidCommand
	}

	inner := map[string]any{"RequestType": string(requestType)}
	for k, v := range fields {
		if k == "RequestType" {
			continue
		}
		inner[k] = v
	}

	uid := uuid.NewString()
	raw, err := plist.Marshal(map[string]any{
		"CommandUUID": uid,
		"Command":     inner,
	})
	if err != nil {
		return nil, err
	}

	cmd := &Command{CommandUUID: uid}
	cmd.Command.RequestType = requestType
	cmd.Raw = raw
	return cmd, nil
}
