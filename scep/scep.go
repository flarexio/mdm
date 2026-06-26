// Package scep wires the SCEP protocol server (micromdm/scep) to our own CA and
// challenge store.
//
// The protocol/CMS layer — parsing the PKIOperation, decrypting the enveloped CSR,
// signing the PKCS#7 response — is reused from micromdm/scep; reimplementing it is
// what option (B) would have been, and it is gnarly. What stays OURS is the
// issuance policy: whether a request is authorized (the one-time challenge) and
// what the certificate looks like (the CA). This is the "CA as a pure signing
// engine, authorization in our hands" architecture.
package scep

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/http"

	kitlog "github.com/go-kit/kit/log"
	scepserver "github.com/micromdm/scep/v2/server"
	scepproto "github.com/smallstep/scep"

	"github.com/flarexio/mdm-server/ca"
	"github.com/flarexio/mdm-server/challenge"
)

// signer adapts our CA + challenge store to micromdm's CSRSignerContext: it
// authorizes the request, then issues the certificate.
type signer struct {
	ca         *ca.CA
	challenges challenge.Store
}

// SignCSRContext validates the one-time challenge, binds it to the CSR identity,
// and issues a device certificate. Returning an error here makes the SCEP server
// reply with a FAILURE status and no certificate.
func (s *signer) SignCSRContext(_ context.Context, m *scepproto.CSRReqMessage) (*x509.Certificate, error) {
	subject, err := s.challenges.Redeem(m.ChallengePassword)
	if err != nil {
		return nil, fmt.Errorf("challenge rejected: %w", err)
	}

	// The challenge was issued for one specific identity; the CSR must match it.
	// Without this cross-check, a challenge leaked for device A could be used to
	// mint a certificate for device B.
	if cn := m.CSR.Subject.CommonName; cn != subject {
		return nil, fmt.Errorf("csr CN %q does not match challenge subject %q", cn, subject)
	}

	return s.ca.Sign(m.CSR, ca.SignOptions{})
}

// NewService builds the SCEP protocol service backed by our CA and challenge store.
func NewService(authority *ca.CA, challenges challenge.Store, logger kitlog.Logger) (scepserver.Service, error) {
	crt, key := authority.SCEPIdentity()
	sgn := &signer{ca: authority, challenges: challenges}
	return scepserver.NewService(crt, key, sgn, scepserver.WithLogger(logger))
}

// NewHandler exposes a SCEP service over HTTP — the endpoint a device's SCEP
// payload URL points at.
func NewHandler(svc scepserver.Service, logger kitlog.Logger) http.Handler {
	return scepserver.MakeHTTPHandler(scepserver.MakeServerEndpoints(svc), svc, logger)
}
