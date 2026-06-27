package http

import (
	"context"
	"crypto/x509"
	"net/http"

	"github.com/flarexio/mdm/enrollment"
)

type contextKey int

const (
	identityContextKey contextKey = iota
	certificateContextKey
)

// IdentityFunc resolves the authenticated enrollment identity of a request. It
// returns the identity, the certificate it came from, and whether resolution
// succeeded. The default implementation (ClientIdentity) reads the verified mTLS
// client certificate; a proxy-header variant could be supplied instead without
// changing the middleware.
type IdentityFunc func(*http.Request) (enrollment.ID, *x509.Certificate, bool)

// ClientIdentity resolves a request's identity from its verified mTLS client
// certificate. The enrollment ID is the certificate's Common Name.
//
// It trusts the certificate ONLY when r.TLS.VerifiedChains is non-empty — i.e. the
// TLS handshake actually verified the chain against the configured ClientCAs
// (ClientAuth = RequireAndVerifyClientCert). A certificate that was merely
// presented but not verified (e.g. RequestClientCert) is rejected: presence is not
// proof.
func ClientIdentity(r *http.Request) (enrollment.ID, *x509.Certificate, bool) {
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		return "", nil, false
	}

	cert := r.TLS.PeerCertificates[0]
	cn := cert.Subject.CommonName
	if cn == "" {
		return "", nil, false
	}

	return enrollment.ID(cn), cert, true
}

// RequireIdentity is middleware that resolves the request identity with identify
// and stores it in the context. Requests without a resolvable identity are rejected
// with 401 before reaching any handler, so downstream code can rely on the identity
// being present and authenticated — never the device-claimed body.
func RequireIdentity(identify IdentityFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, cert, ok := identify(r)
			if !ok {
				http.Error(w, "client certificate required", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), identityContextKey, id)
			ctx = context.WithValue(ctx, certificateContextKey, cert)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// IdentityFromContext returns the authenticated enrollment identity stored by
// RequireIdentity.
func IdentityFromContext(ctx context.Context) (enrollment.ID, bool) {
	id, ok := ctx.Value(identityContextKey).(enrollment.ID)
	return id, ok
}

// CertificateFromContext returns the verified client certificate stored by
// RequireIdentity.
func CertificateFromContext(ctx context.Context) (*x509.Certificate, bool) {
	cert, ok := ctx.Value(certificateContextKey).(*x509.Certificate)
	return cert, ok
}
