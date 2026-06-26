package profile

// AccessRightsAll grants the MDM server every command privilege. Production
// deployments often request only the subset they actually need.
const AccessRightsAll = 8191

// MDMPayload is a com.apple.mdm payload: it enrolls the device into this MDM server
// and, crucially, tells the device WHICH certificate to authenticate with.
type MDMPayload struct {
	PayloadHeader
	ServerURL               string
	CheckInURL              string   `plist:",omitempty"`
	Topic                   string   // APNs topic, derived from the push certificate
	IdentityCertificateUUID string   // MUST equal the identity payload's PayloadUUID
	AccessRights            int      //
	SignMessage             bool     `plist:",omitempty"`
	CheckOutWhenRemoved     bool     `plist:",omitempty"`
	ServerCapabilities      []string `plist:",omitempty"`
}

// NewMDM builds an MDM payload bound to identity — the payload (typically a
// SCEPPayload) that provisions the device's certificate.
//
// Taking identity as a Payload rather than a bare UUID string makes the binding
// impossible to forget or mistype: IdentityCertificateUUID is derived from the very
// payload the device will enroll against, so the device authenticates with exactly
// that certificate.
func NewMDM(identifier, serverURL, checkInURL, topic string, identity Payload) *MDMPayload {
	return &MDMPayload{
		PayloadHeader:           newHeader("com.apple.mdm", identifier, "MDM"),
		ServerURL:               serverURL,
		CheckInURL:              checkInURL,
		Topic:                   topic,
		IdentityCertificateUUID: identity.UUID(),
		AccessRights:            AccessRightsAll,
		SignMessage:             true,
		CheckOutWhenRemoved:     true,
	}
}
