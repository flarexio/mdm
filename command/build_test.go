package command_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm/command"
)

func TestBuild(t *testing.T) {
	cmd, err := command.Build(command.DeviceInformation{
		Queries: []string{"DeviceName", "OSVersion"},
	})
	require.NoError(t, err)

	assert.NotEmpty(t, cmd.CommandUUID, "a fresh UUID must be assigned")
	assert.Equal(t, command.RequestType("DeviceInformation"), cmd.Command.RequestType)

	// Raw must round-trip back through Decode as a valid command.
	got, err := command.Decode(cmd.Raw)
	require.NoError(t, err)
	assert.Equal(t, cmd.CommandUUID, got.CommandUUID)
	assert.Equal(t, command.RequestType("DeviceInformation"), got.Command.RequestType)
	assert.Contains(t, string(cmd.Raw), "DeviceName")
}

func TestBuild_NoFields(t *testing.T) {
	cmd, err := command.Build(command.DeviceLock{})
	require.NoError(t, err)

	_, err = command.Decode(cmd.Raw)
	require.NoError(t, err)
}

func TestNewRequest(t *testing.T) {
	req, err := command.NewRequest("DeviceInformation", map[string]any{
		"Queries": []any{"DeviceName"},
	})
	require.NoError(t, err)
	assert.Equal(t, command.RequestType("DeviceInformation"), req.RequestType())

	cmd, err := command.Build(req)
	require.NoError(t, err)
	assert.Contains(t, string(cmd.Raw), "DeviceName")
}

func TestNewRequest_Unsupported(t *testing.T) {
	_, err := command.NewRequest("NoSuchCommand", nil)
	assert.ErrorIs(t, err, command.ErrUnsupportedCommand)
}

func TestNewRequest_InvalidFields(t *testing.T) {
	// DeviceInformation requires a non-empty Queries array.
	_, err := command.NewRequest("DeviceInformation", map[string]any{})
	assert.ErrorIs(t, err, command.ErrInvalidCommand)
}
