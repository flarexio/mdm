package command_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm/command"
)

// A real-shaped DeviceInformation command, server -> device.
const deviceInfoCommand = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CommandUUID</key>
	<string>0001-deviceinfo</string>
	<key>Command</key>
	<dict>
		<key>RequestType</key>
		<string>DeviceInformation</string>
		<key>Queries</key>
		<array>
			<string>DeviceName</string>
			<string>OSVersion</string>
		</array>
	</dict>
</dict>
</plist>`

func TestDecodeCommand(t *testing.T) {
	cmd, err := command.Decode([]byte(deviceInfoCommand))
	require.NoError(t, err)

	assert.Equal(t, "0001-deviceinfo", cmd.CommandUUID)
	assert.Equal(t, command.RequestType("DeviceInformation"), cmd.Command.RequestType)
	assert.NotEmpty(t, cmd.Raw, "raw plist should be preserved for type-specific handling")
}

func TestDecodeCommand_Invalid(t *testing.T) {
	_, err := command.Decode(nil)
	assert.ErrorIs(t, err, command.ErrEmptyCommand)

	// Has a UUID but no RequestType -> not a usable command.
	missingType := `<plist version="1.0"><dict>
		<key>CommandUUID</key><string>x</string>
		<key>Command</key><dict></dict>
	</dict></plist>`
	_, err = command.Decode([]byte(missingType))
	assert.ErrorIs(t, err, command.ErrInvalidCommand)
}

const acknowledgedResult = `<plist version="1.0">
<dict>
	<key>CommandUUID</key>
	<string>0001-deviceinfo</string>
	<key>Status</key>
	<string>Acknowledged</string>
	<key>UDID</key>
	<string>00008030-001234567890002E</string>
</dict>
</plist>`

const notNowResult = `<plist version="1.0">
<dict>
	<key>Status</key>
	<string>NotNow</string>
	<key>CommandUUID</key>
	<string>0001-deviceinfo</string>
</dict>
</plist>`

// An Idle poll: a result with a Status but no CommandUUID.
const idlePoll = `<plist version="1.0">
<dict>
	<key>Status</key>
	<string>Idle</string>
	<key>UDID</key>
	<string>00008030-001234567890002E</string>
</dict>
</plist>`

func TestDecodeResult(t *testing.T) {
	t.Run("acknowledged", func(t *testing.T) {
		r, err := command.DecodeResult([]byte(acknowledgedResult))
		require.NoError(t, err)

		assert.Equal(t, "0001-deviceinfo", r.CommandUUID)
		assert.Equal(t, command.Acknowledged, r.Status)
		assert.True(t, r.Status.IsTerminal())
		assert.False(t, r.Status.NeedsRetry())
	})

	t.Run("notnow keeps the command for retry", func(t *testing.T) {
		r, err := command.DecodeResult([]byte(notNowResult))
		require.NoError(t, err)

		assert.Equal(t, command.NotNow, r.Status)
		assert.True(t, r.Status.NeedsRetry(), "NotNow must be retried, not discarded")
		assert.False(t, r.Status.IsTerminal())
	})

	t.Run("idle poll has no CommandUUID", func(t *testing.T) {
		r, err := command.DecodeResult([]byte(idlePoll))
		require.NoError(t, err)

		assert.Equal(t, command.Idle, r.Status)
		assert.Empty(t, r.CommandUUID)
	})
}
