package scep_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	kitlog "github.com/go-kit/kit/log"
	scepserver "github.com/micromdm/scep/v2/server"
	scepproto "github.com/smallstep/scep"
	"github.com/smallstep/scep/x509util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm-server/ca"
	"github.com/flarexio/mdm-server/challenge"
	"github.com/flarexio/mdm-server/persistence/inmem"
	"github.com/flarexio/mdm-server/scep"
)

// newDevice emulates a device preparing to enroll: it generates a key pair, a CSR
// carrying the challenge password (a PKCS#10 attribute), and a transient
// self-signed certificate used only to sign the SCEP envelope.
func newDevice(t *testing.T, cn, challengePW string) (*x509.CertificateRequest, *x509.Certificate, *rsa.PrivateKey) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	csrDER, err := x509util.CreateCertificateRequest(rand.Reader, &x509util.CertificateRequest{
		CertificateRequest: x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}},
		ChallengePassword:  challengePW,
	}, key)
	require.NoError(t, err)
	csr, err := x509.ParseCertificateRequest(csrDER)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	selfDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	self, err := x509.ParseCertificate(selfDER)
	require.NoError(t, err)

	return csr, self, key
}

// enroll runs one real PKCSReq through the SCEP service and returns the parsed
// response — exercising the full CMS encrypt/sign path on both sides.
func enroll(t *testing.T, svc scepserver.Service, caCert, self *x509.Certificate, csr *x509.CertificateRequest, key *rsa.PrivateKey) *scepproto.PKIMessage {
	t.Helper()

	tmpl := &scepproto.PKIMessage{
		MessageType: scepproto.PKCSReq,
		Recipients:  []*x509.Certificate{caCert},
		SignerKey:   key,
		SignerCert:  self,
	}
	msg, err := scepproto.NewCSRRequest(csr, tmpl)
	require.NoError(t, err)

	respBytes, err := svc.PKIOperation(context.Background(), msg.Raw)
	require.NoError(t, err)

	resp, err := scepproto.ParsePKIMessage(respBytes)
	require.NoError(t, err)

	return resp
}

func newServer(t *testing.T) (*ca.CA, challenge.Store, scepserver.Service) {
	t.Helper()

	authority, err := ca.New("FlareX MDM CA")
	require.NoError(t, err)

	challenges, err := inmem.NewChallengeStore(5 * time.Minute)
	require.NoError(t, err)

	svc, err := scep.NewService(authority, challenges, kitlog.NewNopLogger())
	require.NoError(t, err)

	return authority, challenges, svc
}

func TestEnrollment(t *testing.T) {
	authority, challenges, svc := newServer(t)

	pw, err := challenges.Generate("device-0001")
	require.NoError(t, err)

	csr, self, key := newDevice(t, "device-0001", pw)

	resp := enroll(t, svc, authority.Certificate, self, csr, key)
	require.Equal(t, scepproto.SUCCESS, resp.PKIStatus, "a valid enrollment should succeed")

	// Decrypt the issued certificate out of the SCEP response envelope.
	require.NoError(t, resp.DecryptPKIEnvelope(self, key))
	issued := resp.CertRepMessage.Certificate

	assert.Equal(t, "device-0001", issued.Subject.CommonName)
	assert.NoError(t, issued.CheckSignatureFrom(authority.Certificate), "must be signed by our CA")
	assert.Contains(t, issued.ExtKeyUsage, x509.ExtKeyUsageClientAuth, "device cert is an mTLS client identity")
}

func TestEnrollment_ChallengeIsOneTime(t *testing.T) {
	authority, challenges, svc := newServer(t)

	pw, err := challenges.Generate("device-0001")
	require.NoError(t, err)

	csr, self, key := newDevice(t, "device-0001", pw)
	resp := enroll(t, svc, authority.Certificate, self, csr, key)
	require.Equal(t, scepproto.SUCCESS, resp.PKIStatus)

	// Reusing the same challenge must be rejected: it was consumed on first use.
	csr2, self2, key2 := newDevice(t, "device-0001", pw)
	resp2 := enroll(t, svc, authority.Certificate, self2, csr2, key2)
	assert.Equal(t, scepproto.FAILURE, resp2.PKIStatus, "a reused challenge must fail")
}

func TestEnrollment_RejectsSubjectMismatch(t *testing.T) {
	authority, challenges, svc := newServer(t)

	// Challenge issued for device-0001 ...
	pw, err := challenges.Generate("device-0001")
	require.NoError(t, err)

	// ... but the CSR claims to be device-9999.
	csr, self, key := newDevice(t, "device-9999", pw)
	resp := enroll(t, svc, authority.Certificate, self, csr, key)
	assert.Equal(t, scepproto.FAILURE, resp.PKIStatus, "CN must match the challenge subject")
}
