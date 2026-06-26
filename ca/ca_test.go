package ca_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm-server/ca"
)

// newCSR generates a device key pair and a CSR for it, the way a device would
// on-device before SCEP enrollment.
func newCSR(t *testing.T, commonName string) (*x509.CertificateRequest, *rsa.PrivateKey) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: commonName},
	}, key)
	require.NoError(t, err)

	csr, err := x509.ParseCertificateRequest(der)
	require.NoError(t, err)

	return csr, key
}

func TestSign(t *testing.T) {
	authority, err := ca.New("FlareX MDM CA")
	require.NoError(t, err)

	csr, _ := newCSR(t, "device-0001")

	cert, err := authority.Sign(csr, ca.SignOptions{})
	require.NoError(t, err)

	t.Run("subject carried over from CSR", func(t *testing.T) {
		assert.Equal(t, "device-0001", cert.Subject.CommonName)
	})

	t.Run("issued by the CA", func(t *testing.T) {
		assert.Equal(t, authority.Certificate.Subject.CommonName, cert.Issuer.CommonName)
		assert.NoError(t, cert.CheckSignatureFrom(authority.Certificate))
	})

	t.Run("shaped as an mTLS client identity, not a CA", func(t *testing.T) {
		assert.False(t, cert.IsCA)
		assert.Contains(t, cert.ExtKeyUsage, x509.ExtKeyUsageClientAuth)
	})

	t.Run("chains to the CA for client auth", func(t *testing.T) {
		roots := x509.NewCertPool()
		roots.AddCert(authority.Certificate)

		_, err := cert.Verify(x509.VerifyOptions{
			Roots:     roots,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		})
		assert.NoError(t, err)
	})
}

// TestSign_RejectsForgedCSR proves the proof-of-possession check: a CSR whose
// signature does not match its contents must not be signed.
func TestSign_RejectsForgedCSR(t *testing.T) {
	authority, err := ca.New("FlareX MDM CA")
	require.NoError(t, err)

	csr, _ := newCSR(t, "device-0001")
	csr.Signature[0] ^= 0xff // tamper with the CSR's self-signature

	_, err = authority.Sign(csr, ca.SignOptions{})
	assert.Error(t, err, "a CSR with an invalid signature must be rejected")
}
