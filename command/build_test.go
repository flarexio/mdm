package command_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm/command"
)

func TestBuild(t *testing.T) {
	cmd, err := command.Build(command.DeviceInformation, map[string]any{
		"Queries": []string{"DeviceName", "OSVersion"},
	})
	require.NoError(t, err)

	assert.NotEmpty(t, cmd.CommandUUID, "a fresh UUID must be assigned")
	assert.Equal(t, command.DeviceInformation, cmd.Command.RequestType)

	// Raw must round-trip back through Decode as a valid command.
	got, err := command.Decode(cmd.Raw)
	require.NoError(t, err)
	assert.Equal(t, cmd.CommandUUID, got.CommandUUID)
	assert.Equal(t, command.DeviceInformation, got.Command.RequestType)
	assert.Contains(t, string(cmd.Raw), "DeviceName")
}

func TestBuild_NoFields(t *testing.T) {
	cmd, err := command.Build(command.DeviceLock, nil)
	require.NoError(t, err)

	_, err = command.Decode(cmd.Raw)
	require.NoError(t, err)
}

func TestBuild_RequiresRequestType(t *testing.T) {
	_, err := command.Build("", nil)
	assert.ErrorIs(t, err, command.ErrInvalidCommand)
}
