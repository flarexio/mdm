// Package ca is a minimal X.509 Certificate Authority: it holds a CA key pair and
// signs device CSRs into identity certificates.
//
// In production this role is filled by a real CA such as StepCA (behind its SCEP
// provisioner). Here we own it so the signing policy — what kind of certificate a
// device actually gets — is explicit and under our control.
//
// This package deliberately knows NOTHING about SCEP. SCEP is merely one transport
// for delivering a CSR to a CA; the CA itself just signs CSRs. Keeping them
// decoupled means the same CA could sit behind SCEP, ACME, EST or a manual request.
package ca

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"time"
)

// CA is a certificate authority: a CA certificate plus its private key.
type CA struct {
	Certificate *x509.Certificate
	key         *rsa.PrivateKey
}

// New generates a fresh self-signed CA. Intended for development and tests; a real
// deployment loads a long-lived CA key pair (or delegates issuance to StepCA).
func New(commonName string) (*CA, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// A CA is self-signed: it is both the subject and the issuer of its own cert.
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}

	return &CA{Certificate: cert, key: key}, nil
}

// SCEPIdentity returns the certificate and key the SCEP protocol layer uses to
// decrypt enveloped requests and sign its PKCS#7 responses.
//
// In this learning vehicle that is the CA key pair itself. A production deployment
// would give the SCEP front-end (RA) its own key and keep the CA signing key in an
// HSM, never handing it to a network-facing service.
func (ca *CA) SCEPIdentity() (*x509.Certificate, *rsa.PrivateKey) {
	return ca.Certificate, ca.key
}

// SignOptions tunes the certificate produced by Sign.
type SignOptions struct {
	// TTL is how long the issued certificate is valid. Defaults to one year.
	TTL time.Duration
}

// Sign turns a device CSR into an identity certificate signed by the CA.
//
// The issued certificate is shaped for its purpose: a TLS CLIENT certificate the
// device presents to the MDM server over mTLS — hence ExtKeyUsageClientAuth. We
// verify the CSR's own signature first: that is the proof the requester actually
// holds the private key matching the public key it asks us to certify
// (proof-of-possession). Skipping this check would let a caller get a certificate
// over someone else's public key.
func (ca *CA) Sign(csr *x509.CertificateRequest, opts SignOptions) (*x509.Certificate, error) {
	if err := csr.CheckSignature(); err != nil {
		return nil, errors.New("csr signature invalid: " + err.Error())
	}

	ttl := opts.TTL
	if ttl == 0 {
		ttl = 365 * 24 * time.Hour
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      csr.Subject,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(ttl),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	// Issued by the CA cert, over the CSR's public key, signed by the CA key.
	der, err := x509.CreateCertificate(rand.Reader, template, ca.Certificate, csr.PublicKey, ca.key)
	if err != nil {
		return nil, err
	}

	return x509.ParseCertificate(der)
}

// randomSerial returns a 128-bit positive serial number. RFC 5280 requires serials
// to be positive and unique per issuer; a large random value satisfies both in
// practice without a stateful counter.
func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}
