package command_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm/command"
)

func TestRegistry_Build(t *testing.T) {
	reg := command.NewRegistry()

	cmd, err := reg.Build(command.DeviceInformation, map[string]any{
		"Queries": []string{"DeviceName"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, cmd.CommandUUID)
	assert.Equal(t, command.DeviceInformation, cmd.Command.RequestType)
}

func TestRegistry_UnknownCommand(t *testing.T) {
	reg := command.NewRegistry()
	_, err := reg.Build("NonExistentCommand", nil)
	assert.ErrorIs(t, err, command.ErrUnknownCommand)
}

func TestRegistry_IsRegistered(t *testing.T) {
	reg := command.NewRegistry()
	assert.True(t, reg.IsRegistered(command.DeviceInformation))
	assert.True(t, reg.IsRegistered(command.DeviceLock))
	assert.True(t, reg.IsRegistered(command.EraseDevice))
	assert.False(t, reg.IsRegistered("BogusCommand"))
}

func TestRegistry_Register(t *testing.T) {
	reg := command.NewRegistry()
	// The built-in commands are pre-registered; adding a custom one works.
	assert.False(t, reg.IsRegistered("CustomCommand"))
	reg.Register(command.CommandDefinition{RequestType: "CustomCommand", Description: "Test"})
	assert.True(t, reg.IsRegistered("CustomCommand"))

	cmd, err := reg.Build("CustomCommand", map[string]any{"Key": "value"})
	require.NoError(t, err)
	assert.Equal(t, command.RequestType("CustomCommand"), cmd.Command.RequestType)
}

func TestCommandRespondedEvent_Structure(t *testing.T) {
	cr := &command.CommandResult{
		CommandUUID: "cmd-uuid-1",
		Status:      command.Acknowledged,
		Payload:     map[string]any{"QueryResponses": map[string]any{"DeviceName": "iPhone"}},
	}

	evt := command.NewCommandRespondedEvent("device-0001", cr)
	assert.Equal(t, "command_responded", evt.EventName())
	assert.Equal(t, "commands.device-0001.responded", evt.Topic())

	data, err := json.Marshal(evt)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "mdm:commands", parsed["domain"])
	assert.Equal(t, "device-0001", parsed["enrollment_id"])
	assert.Equal(t, "cmd-uuid-1", parsed["command_uuid"])
	assert.Equal(t, "Acknowledged", parsed["status"])
	assert.NotNil(t, parsed["result"])
}

func TestCommandRespondedEvent_JSONRoundTrip(t *testing.T) {
	cr := &command.CommandResult{
		CommandUUID: "cmd-x",
		Status:      command.Error,
		ErrorChain: []command.ErrorChain{
			{ErrorCode: 1, ErrorDomain: "MDMErrorDomain", LocalizedDescription: "Something failed"},
		},
	}

	evt := command.NewCommandRespondedEvent("device-abc", cr)
	data, err := json.Marshal(evt)
	require.NoError(t, err)

	var parsed command.CommandRespondedEvent
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "device-abc", parsed.EnrollmentID)
	assert.Equal(t, "cmd-x", parsed.CommandUUID)
	assert.Equal(t, command.Error, parsed.Status)
	require.NotNil(t, parsed.Result)
	require.Len(t, parsed.Result.ErrorChain, 1)
	assert.Equal(t, 1, parsed.Result.ErrorChain[0].ErrorCode)
}
