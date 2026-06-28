package profile

// CertificatePayload is a com.apple.security.root payload: it installs a CA
// certificate into the device's trust. Carrying the FlareX root here lets the
// device trust the MDM and SCEP servers' TLS certificates (which chain to it)
// without those endpoints needing publicly-trusted (Let's Encrypt) certs.
//
// PayloadContent is the raw DER certificate; micromdm/plist marshals a []byte as
// a plist <data> element, which is what Apple expects here.
type CertificatePayload struct {
	PayloadHeader
	PayloadContent             []byte
	PayloadCertificateFileName string `plist:",omitempty"`
}

// NewCertificate builds a trusted-root payload from a DER-encoded certificate.
func NewCertificate(identifier, fileName string, der []byte) *CertificatePayload {
	return &CertificatePayload{
		PayloadHeader:              newHeader("com.apple.security.root", identifier, "Trusted Root Certificate"),
		PayloadContent:             der,
		PayloadCertificateFileName: fileName,
	}
}
