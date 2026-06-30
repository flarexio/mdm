package main

import (
	"strings"
	"testing"
	"time"

	"github.com/flarexio/mdm/command"
)

func TestRenderStates(t *testing.T) {
	m := newModel("http://localhost", nil, 30*time.Second)
	m.subject = "device-1"

	for _, s := range []state{stateToken, stateDevices, stateCommand, stateQueries, stateWaiting} {
		m.state = s
		if m.render() == "" {
			t.Fatalf("state %d rendered empty", s)
		}
	}
}

func TestResultView(t *testing.T) {
	m := newModel("http://localhost", nil, time.Second)
	m.result = &command.RespondedEvent{
		EnrollmentID: "device-1",
		CommandUUID:  "uuid-1",
		RequestType:  "DeviceInformation",
		Status:       command.Acknowledged,
		Response:     map[string]any{"QueryResponses": map[string]any{"DeviceName": "My iPhone"}},
	}

	out := m.resultView()
	if !strings.Contains(out, "DeviceInformation") || !strings.Contains(out, "My iPhone") {
		t.Fatalf("result view missing expected fields: %q", out)
	}

	// timeout path: no result, status set.
	m.result = nil
	m.status = "timed out"
	if !strings.Contains(m.resultView(), "timed out") {
		t.Fatal("timeout result view should show the status")
	}
}
