package command_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm/command"
)

const deviceInfoResult = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
	<key>CommandUUID</key><string>0001-deviceinfo</string>
	<key>Status</key><string>Acknowledged</string>
	<key>QueryResponses</key><dict>
		<key>DeviceName</key><string>My iPhone</string>
		<key>OSVersion</key><string>17.0</string>
		<key>BatteryLevel</key><real>0.85</real>
	</dict>
</dict></plist>`

func TestDecodeResponse_DeviceInformation(t *testing.T) {
	got, err := command.DecodeResponse("DeviceInformation", []byte(deviceInfoResult))
	require.NoError(t, err)

	resp, ok := got.(command.DeviceInformationResponse)
	require.True(t, ok, "expected a DeviceInformationResponse, got %T", got)
	assert.Equal(t, "My iPhone", resp.QueryResponses["DeviceName"])
	assert.Equal(t, "17.0", resp.QueryResponses["OSVersion"])
	assert.Contains(t, resp.QueryResponses, "BatteryLevel")
}

func TestDecodeResponse_NoDecoder(t *testing.T) {
	// DeviceLock has no typed result; an unknown command has none either. Both fall
	// back to nil so the caller uses the generic Result.
	for _, rt := range []command.RequestType{"DeviceLock", "NoSuchCommand"} {
		got, err := command.DecodeResponse(rt, []byte(deviceInfoResult))
		require.NoError(t, err)
		assert.Nil(t, got)
	}
}
