package profile

// Key usage bit values for a SCEP payload's "Key Usage" field.
const (
	KeyUsageSigning    = 1
	KeyUsageEncryption = 4
	KeyUsageBoth       = KeyUsageSigning | KeyUsageEncryption // 5
)

// SCEPPayload is a com.apple.security.scep payload. It instructs the device to
// generate a key pair ON DEVICE and enroll for an identity certificate via SCEP.
// The private key never leaves the device; only the issued certificate, which then
// serves as the device's mTLS identity to the MDM server, identifies it.
type SCEPPayload struct {
	PayloadHeader
	PayloadContent SCEPContent
}

// SCEPContent mirrors Apple's SCEP payload dictionary.
//
// NOTE: the key names follow Apple's (inconsistent) schema exactly — "Keysize" is
// one word while "Key Type" and "Key Usage" contain a space — so they are tagged
// explicitly. Getting these wrong means the device silently ignores the setting.
type SCEPContent struct {
	URL        string
	Name       string       `plist:",omitempty"`         // CA instance name (CA-IDENT)
	Subject    [][][]string `plist:",omitempty"`         // see SubjectName
	Challenge  string       `plist:",omitempty"`         // authorizes issuance; use a one-time value
	Keysize    int          `plist:"Keysize,omitempty"`  //
	KeyType    string       `plist:"Key Type,omitempty"` // "RSA"
	KeyUsage   int          `plist:"Key Usage,omitempty"`
	Retries    int          `plist:",omitempty"`
	RetryDelay int          `plist:",omitempty"`
}

// SubjectName builds the awkward array-of-RDNs structure Apple's SCEP payload uses
// for the certificate subject. Each pair becomes one RDN of [type, value]:
//
//	SubjectName([2]string{"O", "FlareX"}, [2]string{"CN", "%HardwareUUID%"})
//	  -> [[["O","FlareX"]], [["CN","%HardwareUUID%"]]]
//
// The CN is commonly templated with a device variable such as "%HardwareUUID%",
// which the device expands to its own value at enrollment time.
func SubjectName(pairs ...[2]string) [][][]string {
	subject := make([][][]string, 0, len(pairs))
	for _, p := range pairs {
		subject = append(subject, [][]string{{p[0], p[1]}})
	}
	return subject
}

// NewSCEP builds a SCEP payload from the given content.
func NewSCEP(identifier string, content SCEPContent) *SCEPPayload {
	return &SCEPPayload{
		PayloadHeader:  newHeader("com.apple.security.scep", identifier, "SCEP Enrollment"),
		PayloadContent: content,
	}
}
