// Package http contains HTTP transport adapters for the MDM server.
package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"

	"github.com/flarexio/mdm-server/challenge"
)

// scepChallengeRequest mirrors the subset of smallstep/certificates'
// webhook.RequestBody that we need. The webhook is a stable JSON contract, so we
// model it directly rather than depend on the (large) certificates module.
type scepChallengeRequest struct {
	SCEPChallenge          string `json:"scepChallenge"`
	SCEPTransactionID      string `json:"scepTransactionID"`
	X509CertificateRequest *struct {
		Raw []byte `json:"raw"` // the CSR in DER form (JSON base64)
	} `json:"x509CertificateRequest"`
}

// scepChallengeResponse is the webhook.ResponseBody StepCA expects.
type scepChallengeResponse struct {
	Allow bool `json:"allow"`
}

// SCEPChallengeHandler validates StepCA's SCEP challenge webhook. StepCA calls this
// before signing a SCEP request; we redeem the one-time challenge and bind it to
// the CSR's subject, then allow or deny issuance. This is the production shape of
// the same policy the embedded signer enforces — but with StepCA as the real CA.
//
// secret is the raw HMAC key shared with StepCA (the base64 value in StepCA's
// webhook config, already decoded). Every request is authenticated by verifying the
// X-Smallstep-Signature header (hex HMAC-SHA256 over the raw body) so that only
// StepCA — not an arbitrary caller — can drive issuance decisions.
func SCEPChallengeHandler(challenges challenge.Store, secret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}

		// Authenticate before doing anything that has side effects (redeeming the
		// challenge): a forged request must not be able to consume a challenge.
		if !validSignature(r.Header.Get("X-Smallstep-Signature"), body, secret) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		var req scepChallengeRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(scepChallengeResponse{Allow: authorize(challenges, req)})
	}
}

// authorize redeems the one-time challenge and binds it to the CSR subject. Any
// failure is a deny (allow=false): StepCA only wants a yes/no, and the challenge is
// consumed either way (single use).
func authorize(challenges challenge.Store, req scepChallengeRequest) bool {
	subject, err := challenges.Redeem(req.SCEPChallenge)
	if err != nil {
		return false // unknown, expired or already-used challenge
	}

	if req.X509CertificateRequest == nil {
		return false
	}

	csr, err := x509.ParseCertificateRequest(req.X509CertificateRequest.Raw)
	if err != nil {
		return false
	}

	// The challenge was issued for one identity; the CSR must match it. Without
	// this, a challenge leaked for device A could mint a certificate for device B.
	return csr.Subject.CommonName == subject
}

// validSignature reports whether sigHeader is a valid hex HMAC-SHA256 of body under
// secret, compared in constant time.
func validSignature(sigHeader string, body, secret []byte) bool {
	got, err := hex.DecodeString(sigHeader)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := mac.Sum(nil)

	return hmac.Equal(got, want)
}
