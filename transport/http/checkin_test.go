package http_test

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mdm "github.com/flarexio/mdm-server"
	"github.com/flarexio/mdm-server/enrollment"
	"github.com/flarexio/mdm-server/persistence/inmem"
	transhttp "github.com/flarexio/mdm-server/transport/http"
)

const authenticatePlist = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
	<key>MessageType</key><string>Authenticate</string>
	<key>UDID</key><string>UDID-the-device-claims</string>
	<key>Topic</key><string>com.apple.mgmt.External.abc</string>
</dict></plist>`

const tokenUpdatePlist = `<plist version="1.0"><dict>
	<key>MessageType</key><string>TokenUpdate</string>
	<key>UDID</key><string>UDID-the-device-claims</string>
	<key>Topic</key><string>com.apple.mgmt.External.abc</string>
	<key>PushMagic</key><string>MAGIC-123</string>
	<key>Token</key><data>AQIDBA==</data>
</dict></plist>`

// checkinReq builds a check-in request authenticated by a verified client cert
// with the given CN.
func checkinReq(cn, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/checkin", strings.NewReader(body))
	leaf := &x509.Certificate{Subject: pkix.Name{CommonName: cn}}
	r.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{leaf},
		VerifiedChains:   [][]*x509.Certificate{{leaf}},
	}
	return r
}

func mdmHandler(t *testing.T) (http.Handler, enrollment.Repository) {
	t.Helper()

	repo, err := inmem.NewEnrollmentRepository()
	require.NoError(t, err)

	svc := mdm.NewService(repo)
	h := transhttp.RequireIdentity(transhttp.ClientIdentity)(transhttp.CheckInHandler(svc))

	return h, repo
}

func TestCheckInHandler(t *testing.T) {
	h, repo := mdmHandler(t)

	// Authenticate over mTLS: cert CN is device-from-cert, body claims a different UDID.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, checkinReq("device-from-cert", authenticatePlist))
	require.Equal(t, http.StatusOK, rec.Code)

	t.Run("stored under the cert identity, not the body UDID", func(t *testing.T) {
		e, err := repo.Find("device-from-cert")
		require.NoError(t, err)
		assert.Equal(t, enrollment.Pending, e.Status)
		assert.Equal(t, "UDID-the-device-claims", e.UDID, "body UDID kept as data")

		// Nothing was stored under the device-claimed UDID as an identity.
		_, err = repo.Find("UDID-the-device-claims")
		assert.ErrorIs(t, err, enrollment.ErrEnrollmentNotFound)
	})

	t.Run("token update enrolls the same device", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, checkinReq("device-from-cert", tokenUpdatePlist))
		require.Equal(t, http.StatusOK, rec.Code)

		e, err := repo.Find("device-from-cert")
		require.NoError(t, err)
		assert.Equal(t, enrollment.Enrolled, e.Status)
		assert.True(t, e.CanPush())
	})
}

func TestCheckInHandler_RejectsUnauthenticated(t *testing.T) {
	h, _ := mdmHandler(t)

	// No client cert -> RequireIdentity rejects before the handler runs.
	req := httptest.NewRequest(http.MethodPut, "/checkin", strings.NewReader(authenticatePlist))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCheckInHandler_BadBody(t *testing.T) {
	h, _ := mdmHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, checkinReq("device-from-cert", "not a plist"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
