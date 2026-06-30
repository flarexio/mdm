package command

import "github.com/micromdm/plist"

// DeviceInformationResponse is the device's reply to DeviceInformation: the
// requested properties keyed by query name. Values are heterogeneous (strings,
// numbers, booleans, nested dicts), so they are kept as decoded plist values; a
// later typed layer can narrow the common keys.
type DeviceInformationResponse struct {
	QueryResponses map[string]any
}

// DecodeResponse decodes a DeviceInformation result plist. The receiver is unused
// (decoding needs no request state); it exists so DeviceInformation is the single
// type that both builds the command and decodes its result.
func (DeviceInformation) DecodeResponse(raw []byte) (any, error) {
	var r struct {
		QueryResponses map[string]any
	}
	if err := plist.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return DeviceInformationResponse{QueryResponses: r.QueryResponses}, nil
}
