package command_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm/command"
)

func TestDecodeCommandResult_Acknowledged(t *testing.T) {
	raw := []byte(`<plist version="1.0"><dict>
		<key>CommandUUID</key><string>cmd-1</string>
		<key>Status</key><string>Acknowledged</string>
		<key>UDID</key><string>00008030-001234567890002E</string>
		<key>QueryResponses</key>
		<dict>
			<key>DeviceName</key><string>iPhone</string>
			<key>OSVersion</key><string>17.0</string>
		</dict>
	</dict></plist>`)

	result, err := command.DecodeResult(raw)
	require.NoError(t, err)

	cr, err := command.DecodeCommandResult(result)
	require.NoError(t, err)

	assert.Equal(t, "cmd-1", cr.CommandUUID)
	assert.Equal(t, command.Acknowledged, cr.Status)
	assert.NotNil(t, cr.Payload)

	qr, ok := cr.Payload["QueryResponses"].(map[string]any)
	require.True(t, ok, "QueryResponses should be in the typed payload")
	assert.Equal(t, "iPhone", qr["DeviceName"])
	assert.Equal(t, "17.0", qr["OSVersion"])

	// Envelope fields must be stripped.
	assert.NotContains(t, cr.Payload, "Status")
	assert.NotContains(t, cr.Payload, "CommandUUID")
	assert.NotContains(t, cr.Payload, "UDID")
}

func TestDecodeCommandResult_Error(t *testing.T) {
	raw := []byte(`<plist version="1.0"><dict>
		<key>CommandUUID</key><string>cmd-err</string>
		<key>Status</key><string>Error</string>
		<key>UDID</key><string>00008030-001234567890002E</string>
		<key>ErrorChain</key>
		<array>
			<dict>
				<key>ErrorCode</key><integer>1</integer>
				<key>ErrorDomain</key><string>MDMErrorDomain</string>
				<key>LocalizedDescription</key><string>Command failed</string>
			</dict>
		</array>
	</dict></plist>`)

	result, err := command.DecodeResult(raw)
	require.NoError(t, err)

	cr, err := command.DecodeCommandResult(result)
	require.NoError(t, err)

	assert.Equal(t, "cmd-err", cr.CommandUUID)
	assert.Equal(t, command.Error, cr.Status)
	require.Len(t, cr.ErrorChain, 1)
	assert.Equal(t, 1, cr.ErrorChain[0].ErrorCode)
	assert.Equal(t, "MDMErrorDomain", cr.ErrorChain[0].ErrorDomain)
}

func TestDecodeCommandResult_NotNow(t *testing.T) {
	raw := []byte(`<plist version="1.0"><dict>
		<key>CommandUUID</key><string>cmd-nn</string>
		<key>Status</key><string>NotNow</string>
	</dict></plist>`)

	result, err := command.DecodeResult(raw)
	require.NoError(t, err)

	cr, err := command.DecodeCommandResult(result)
	require.NoError(t, err)

	assert.Equal(t, "cmd-nn", cr.CommandUUID)
	assert.Equal(t, command.NotNow, cr.Status)
	assert.Empty(t, cr.Payload)
}

func TestDecodeCommandResult_IdleDoesNotEmit(t *testing.T) {
	// Idle results have no CommandUUID and are handled at the call site
	// (they produce no event). Verify the result model still decodes correctly.
	raw := []byte(`<plist version="1.0"><dict>
		<key>Status</key><string>Idle</string>
	</dict></plist>`)

	result, err := command.DecodeResult(raw)
	require.NoError(t, err)
	assert.Equal(t, command.Idle, result.Status)
	assert.Empty(t, result.CommandUUID)
}

func TestDecodeCommandResult_NoPayloadOnEmptyResult(t *testing.T) {
	// A terminal result with no type-specific data.
	raw := []byte(`<plist version="1.0"><dict>
		<key>CommandUUID</key><string>cmd-lock</string>
		<key>Status</key><string>Acknowledged</string>
	</dict></plist>`)

	result, err := command.DecodeResult(raw)
	require.NoError(t, err)

	cr, err := command.DecodeCommandResult(result)
	require.NoError(t, err)

	assert.Equal(t, "cmd-lock", cr.CommandUUID)
	assert.Empty(t, cr.Payload, "no type-specific payload when result has only envelope fields")
}
