package push

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"net/http"
)

// oidUID is the X.520 userID (UID) attribute. The MDM push certificate carries the
// APNs topic in this subject attribute.
var oidUID = asn1.ObjectIdentifier{0, 9, 2342, 19200300, 100, 1, 1}

// NewCertClient builds a certificate-based APNs client: cert is the MDM push
// certificate (with its private key), presented to APNs as the TLS client
// certificate. This is the standard MDM push authentication — no bearer token.
func NewCertClient(host string, cert tls.Certificate) *Client {
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				MinVersion:   tls.VersionTLS12,
			},
			ForceAttemptHTTP2: true,
		},
	}

	return NewClient(host, nil, httpClient)
}

// TopicFromCertificate extracts the APNs topic from an MDM push certificate. The
// topic is not configured separately — it IS the certificate's subject UID
// (e.g. "com.apple.mgmt.External.<uuid>"), so reading it from the cert keeps the
// two from drifting apart.
func TopicFromCertificate(cert tls.Certificate) (string, error) {
	leaf := cert.Leaf
	if leaf == nil {
		if len(cert.Certificate) == 0 {
			return "", errors.New("certificate is empty")
		}

		parsed, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return "", err
		}
		leaf = parsed
	}

	for _, name := range leaf.Subject.Names {
		if name.Type.Equal(oidUID) {
			if topic, ok := name.Value.(string); ok && topic != "" {
				return topic, nil
			}
		}
	}

	return "", errors.New("push certificate has no UID (topic)")
}
