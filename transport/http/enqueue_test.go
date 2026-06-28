package http_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm"
	"github.com/flarexio/mdm/command"
	"github.com/flarexio/mdm/enrollment"
	"github.com/flarexio/mdm/persistence/inmem"
	transhttp "github.com/flarexio/mdm/transport/http"
)

func enqueueSetup(t *testing.T) (http.Handler, mdm.Service) {
	t.Helper()

	repo, err := inmem.NewEnrollmentRepository()
	require.NoError(t, err)
	queue, err := inmem.NewCommandQueue()
	require.NoError(t, err)

	svc := mdm.NewService(repo, queue, nopPusher{})
	return transhttp.EnqueueHandler(svc), svc
}

func TestEnqueueHandler(t *testing.T) {
	h, svc := enqueueSetup(t)

	body := `{"requestType":"DeviceInformation","command":{"Queries":["DeviceName"]}}`
	req := httptest.NewRequest(http.MethodPost, "/enqueue/device-0001", strings.NewReader(body))
	req.SetPathValue("subject", "device-0001")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), "commandUUID")

	// The command must now be deliverable on the device's next poll.
	next, err := svc.Command(enrollment.ID("device-0001"), &command.Result{Status: command.Idle})
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.Equal(t, command.DeviceInformation, next.Command.RequestType)
}

func TestEnqueueHandler_BadRequestType(t *testing.T) {
	h, _ := enqueueSetup(t)

	req := httptest.NewRequest(http.MethodPost, "/enqueue/device-0001", strings.NewReader(`{}`))
	req.SetPathValue("subject", "device-0001")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
