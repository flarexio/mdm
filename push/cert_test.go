package push_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm/push"
)

var oidUID = asn1.ObjectIdentifier{0, 9, 2342, 19200300, 100, 1, 1}

// makePushCert builds a self-signed certificate shaped like an MDM push cert: its
// subject carries the APNs topic in the UID attribute.
func makePushCert(t *testing.T, topic string) tls.Certificate {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	subject := pkix.Name{CommonName: "APSP:test"}
	if topic != "" {
		subject.ExtraNames = []pkix.AttributeTypeAndValue{{Type: oidUID, Value: topic}}
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      subject,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

func TestTopicFromCertificate(t *testing.T) {
	cert := makePushCert(t, "com.apple.mgmt.External.0001")

	topic, err := push.TopicFromCertificate(cert)
	require.NoError(t, err)
	assert.Equal(t, "com.apple.mgmt.External.0001", topic)
}

func TestTopicFromCertificate_NoUID(t *testing.T) {
	cert := makePushCert(t, "") // no UID attribute

	_, err := push.TopicFromCertificate(cert)
	assert.Error(t, err)
}

func TestNewCertClient_PresentsTheCertWithNoBearer(t *testing.T) {
	cert := makePushCert(t, "com.apple.mgmt.External.0001")

	client := push.NewCertClient(push.HostProduction, cert)
	require.NotNil(t, client)
}
