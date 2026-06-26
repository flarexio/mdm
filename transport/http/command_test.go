package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mdm "github.com/flarexio/mdm-server"
	"github.com/flarexio/mdm-server/command"
	"github.com/flarexio/mdm-server/enrollment"
	"github.com/flarexio/mdm-server/persistence/inmem"
	"github.com/flarexio/mdm-server/push"
	transhttp "github.com/flarexio/mdm-server/transport/http"
)

// nopPusher is a no-op Pusher for transport tests that do not exercise APNs.
type nopPusher struct{}

func (nopPusher) Push(context.Context, push.Target) error { return nil }

func deviceInfoCommand(t *testing.T, uuid string) *command.Command {
	t.Helper()
	c, err := command.Decode([]byte(`<plist version="1.0"><dict>
		<key>CommandUUID</key><string>` + uuid + `</string>
		<key>Command</key><dict><key>RequestType</key><string>DeviceInformation</string></dict>
	</dict></plist>`))
	require.NoError(t, err)
	return c
}

const idlePlist = `<plist version="1.0"><dict>
	<key>Status</key><string>Idle</string>
</dict></plist>`

func resultPlist(uuid, status string) string {
	return `<plist version="1.0"><dict>
		<key>CommandUUID</key><string>` + uuid + `</string>
		<key>Status</key><string>` + status + `</string>
	</dict></plist>`
}

func commandSetup(t *testing.T) (http.Handler, mdm.Service) {
	t.Helper()

	repo, err := inmem.NewEnrollmentRepository()
	require.NoError(t, err)
	queue, err := inmem.NewCommandQueue()
	require.NoError(t, err)

	svc := mdm.NewService(repo, queue, nopPusher{})
	h := transhttp.RequireIdentity(transhttp.ClientIdentity)(transhttp.CommandHandler(svc))

	return h, svc
}

// poll sends one command-channel turn (a Result or Idle plist) as device cn.
func poll(t *testing.T, h http.Handler, cn, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, checkinReq(cn, body))
	require.Equal(t, http.StatusOK, rec.Code)
	return rec
}

func TestCommandLoop(t *testing.T) {
	h, svc := commandSetup(t)
	const id enrollment.ID = "device-0001"

	require.NoError(t, svc.Enqueue(id, deviceInfoCommand(t, "cmd-1")))
	require.NoError(t, svc.Enqueue(id, deviceInfoCommand(t, "cmd-2")))

	t.Run("idle poll returns the first command", func(t *testing.T) {
		rec := poll(t, h, "device-0001", idlePlist)
		assert.Contains(t, rec.Body.String(), "cmd-1")
		assert.Equal(t, "application/x-apple-aspen-mdm", rec.Header().Get("Content-Type"))
	})

	t.Run("acknowledging advances to the next command", func(t *testing.T) {
		rec := poll(t, h, "device-0001", resultPlist("cmd-1", "Acknowledged"))
		assert.Contains(t, rec.Body.String(), "cmd-2")
	})

	t.Run("empty queue returns an empty 200", func(t *testing.T) {
		rec := poll(t, h, "device-0001", resultPlist("cmd-2", "Acknowledged"))
		assert.Empty(t, rec.Body.String(), "empty body tells the device there is nothing more")
	})
}

func TestCommandLoop_NotNowRetriesLater(t *testing.T) {
	h, svc := commandSetup(t)
	const id enrollment.ID = "device-0001"

	require.NoError(t, svc.Enqueue(id, deviceInfoCommand(t, "cmd-1")))
	require.NoError(t, svc.Enqueue(id, deviceInfoCommand(t, "cmd-2")))

	// First poll hands out cmd-1.
	assert.Contains(t, poll(t, h, "device-0001", idlePlist).Body.String(), "cmd-1")

	// Device defers cmd-1 with NotNow; the same connection moves on to cmd-2.
	assert.Contains(t, poll(t, h, "device-0001", resultPlist("cmd-1", "NotNow")).Body.String(), "cmd-2")

	// A later poll retries the deferred cmd-1.
	assert.Contains(t, poll(t, h, "device-0001", idlePlist).Body.String(), "cmd-1")
}
