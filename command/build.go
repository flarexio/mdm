package command

import (
	"github.com/google/uuid"
	"github.com/micromdm/plist"
)

// Build assembles a deliverable command from a Request, assigning a fresh
// CommandUUID. The Request supplies the type-specific Fields placed next to
// RequestType inside the inner Command dict; RequestType comes from the Request
// and cannot be overridden.
//
// The marshalled plist is kept in Raw, exactly as a decoded command would be, so
// the queue and command channel treat built and device-bound commands alike.
func Build(req Request) (*Command, error) {
	requestType := req.RequestType()
	if requestType == "" {
		return nil, ErrInvalidCommand
	}

	inner := map[string]any{"RequestType": string(requestType)}
	for k, v := range req.Fields() {
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
