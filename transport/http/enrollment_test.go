package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm"
	"github.com/flarexio/mdm/checkin"
	"github.com/flarexio/mdm/enrollment"
	"github.com/flarexio/mdm/persistence/inmem"
	transhttp "github.com/flarexio/mdm/transport/http"
)

func enrollmentSetup(t *testing.T) mdm.Service {
	t.Helper()

	repo, err := inmem.NewEnrollmentRepository()
	require.NoError(t, err)
	queue, err := inmem.NewCommandQueue()
	require.NoError(t, err)

	svc := mdm.NewService(repo, queue, nopPusher{})

	const id enrollment.ID = "device-0001"
	require.NoError(t, svc.Authenticate(id, &checkin.Authenticate{
		Enrollment: checkin.Enrollment{UDID: "UDID-1"},
	}))
	require.NoError(t, svc.TokenUpdate(id, &checkin.TokenUpdate{
		Push: checkin.Push{Topic: "com.apple.mgmt.External.x", PushMagic: "magic", Token: []byte{0x01}},
	}))

	return svc
}

func TestEnrollmentsHandler(t *testing.T) {
	svc := enrollmentSetup(t)

	rec := httptest.NewRecorder()
	transhttp.EnrollmentsHandler(svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/enrollments", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "device-0001")
	assert.Contains(t, rec.Body.String(), `"status":"enrolled"`)
}

func TestEnrollmentHandler(t *testing.T) {
	svc := enrollmentSetup(t)

	t.Run("found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/enrollments/device-0001", nil)
		req.SetPathValue("subject", "device-0001")
		rec := httptest.NewRecorder()
		transhttp.EnrollmentHandler(svc).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"can_push":true`)
		assert.NotContains(t, rec.Body.String(), "token", "raw push token must not be exposed")
	})

	t.Run("unknown subject is 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/enrollments/nope", nil)
		req.SetPathValue("subject", "nope")
		rec := httptest.NewRecorder()
		transhttp.EnrollmentHandler(svc).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}
