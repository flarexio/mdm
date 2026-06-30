package command

import "fmt"

func init() {
	Register(deviceInformation, func(fields map[string]any) (Request, error) {
		queries, err := stringSlice(fields, "Queries")
		if err != nil {
			return nil, err
		}
		if len(queries) == 0 {
			return nil, fmt.Errorf("%w: DeviceInformation requires Queries", ErrInvalidCommand)
		}
		return DeviceInformation{Queries: queries}, nil
	}, DeviceInformation{})

	Register(deviceLock, func(fields map[string]any) (Request, error) {
		return DeviceLock{
			Message:     stringValue(fields, "Message"),
			PhoneNumber: stringValue(fields, "PhoneNumber"),
			PIN:         stringValue(fields, "PIN"),
		}, nil
	}, nil)
}

// DeviceInformation queries the device for the named properties.
type DeviceInformation struct {
	Queries []string
}

func (DeviceInformation) RequestType() RequestType { return deviceInformation }

func (c DeviceInformation) Fields() map[string]any {
	return map[string]any{"Queries": c.Queries}
}

// DeviceLock locks the device immediately, optionally showing a message on the
// lock screen and (on macOS) setting a PIN.
type DeviceLock struct {
	Message     string
	PhoneNumber string
	PIN         string
}

func (DeviceLock) RequestType() RequestType { return deviceLock }

func (c DeviceLock) Fields() map[string]any {
	fields := map[string]any{}
	putString(fields, "Message", c.Message)
	putString(fields, "PhoneNumber", c.PhoneNumber)
	putString(fields, "PIN", c.PIN)
	return fields
}

// putString adds key=v to fields only when v is non-empty, so optional fields are
// omitted from the command plist.
func putString(fields map[string]any, key, v string) {
	if v != "" {
		fields[key] = v
	}
}

// stringValue reads an optional string field, returning "" when absent or not a
// string.
func stringValue(fields map[string]any, key string) string {
	v, _ := fields[key].(string)
	return v
}

// stringSlice reads a []string field, tolerating the []any a JSON decode produces.
func stringSlice(fields map[string]any, key string) ([]string, error) {
	switch v := fields[key].(type) {
	case nil:
		return nil, nil
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("%w: %q must be a string array", ErrInvalidCommand, key)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: %q must be a string array", ErrInvalidCommand, key)
	}
}
