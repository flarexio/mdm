// Package command models the Apple MDM "command and report results" protocol:
// the commands a server delivers to a device and the results a device reports back.
//
// Apple defines a large, open-ended and ever-growing set of commands. Rather than
// trying to enumerate every command and its payload, we decode only the generic
// "envelope" (the fields common to ALL commands) and keep the original plist bytes
// around for command-specific handling. This "parse the envelope, keep the raw"
// approach is the correct way to deal with an open protocol you do not fully control.
package command

import (
	"errors"

	"github.com/micromdm/plist"
)

var (
	ErrEmptyCommand   = errors.New("empty command bytes")
	ErrInvalidCommand = errors.New("invalid command: missing CommandUUID or RequestType")
)

// RequestType identifies the kind of MDM command, e.g. "DeviceInformation" or
// "InstallProfile". It is intentionally a string and not an enum: the set is
// open-ended, and the string is the actual on-the-wire contract with Apple.
type RequestType string

// The request-type strings for the commands this server implements. They are
// unexported: callers select a command through its typed Request (see registry.go
// and requests.go), not by naming a RequestType string.
const (
	deviceInformation RequestType = "DeviceInformation"
	deviceLock        RequestType = "DeviceLock"
	eraseDevice       RequestType = "EraseDevice"
)

// Command is the generic MDM command envelope. Only CommandUUID and the nested
// RequestType are decoded; the full command (including its type-specific payload)
// is preserved in Raw.
//
// The nested Command struct mirrors the plist shape Apple uses:
//
//	<dict>
//	  <key>CommandUUID</key><string>...</string>
//	  <key>Command</key>
//	  <dict><key>RequestType</key><string>...</string> ... </dict>
//	</dict>
type Command struct {
	CommandUUID string
	Command     struct {
		RequestType RequestType
	}
	Raw []byte `plist:"-"` // original plist; excluded from (un)marshalling
}

// Decode parses a raw command plist into its envelope and validates the two fields
// every command must carry. CommandUUID is what later lets us correlate the result
// (which arrives in a separate, possibly much-later request) back to this command.
func Decode(raw []byte) (*Command, error) {
	if len(raw) == 0 {
		return nil, ErrEmptyCommand
	}

	var cmd Command
	if err := plist.Unmarshal(raw, &cmd); err != nil {
		return nil, err
	}
	cmd.Raw = raw

	if cmd.CommandUUID == "" || cmd.Command.RequestType == "" {
		return nil, ErrInvalidCommand
	}

	return &cmd, nil
}
