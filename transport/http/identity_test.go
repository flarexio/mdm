package http_test

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm-server/enrollment"
	transhttp "github.com/flarexio/mdm-server/transport/http"
)

// reqWithTLS builds a request whose TLS state carries a client certificate with the
// given CN. verified controls whether the chain is marked as verified by the
// handshake (VerifiedChains populated).
func reqWithTLS(cn string, verified bool) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/mdm/checkin", nil)
	if cn == "" && !verified {
		return r // no TLS at all
	}

	leaf := &x509.Certificate{Subject: pkix.Name{CommonName: cn}}
	state := &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}
	if verified {
		state.VerifiedChains = [][]*x509.Certificate{{leaf}}
	}
	r.TLS = state

	return r
}

// captureIdentity is a handler that records the identity it sees in the context.
func captureIdentity(seen *enrollment.ID, ok *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen, *ok = transhttp.IdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireIdentity(t *testing.T) {
	mw := transhttp.RequireIdentity(transhttp.ClientIdentity)

	t.Run("passes a verified client cert identity downstream", func(t *testing.T) {
		var seen enrollment.ID
		var ok bool
		h := mw(captureIdentity(&seen, &ok))

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, reqWithTLS("device-0001", true))

		require.Equal(t, http.StatusOK, rec.Code)
		require.True(t, ok, "identity should be present in context")
		assert.Equal(t, enrollment.ID("device-0001"), seen)
	})

	t.Run("rejects a request with no TLS", func(t *testing.T) {
		h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("handler must not run without an identity")
		}))

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, reqWithTLS("", false))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("rejects an unverified client cert", func(t *testing.T) {
		h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("an unverified certificate must not be trusted")
		}))

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, reqWithTLS("device-0001", false)) // presented but not verified
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("rejects a verified cert with an empty CN", func(t *testing.T) {
		h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("handler must not run without an identity")
		}))

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, reqWithTLS("", true))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}
