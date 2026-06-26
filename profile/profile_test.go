package profile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm-server/profile"
)

func TestEnrollmentProfile(t *testing.T) {
	scep := profile.NewSCEP("io.flarex.mdm.enroll.scep", profile.SCEPContent{
		URL:       "https://scep.flarex.io/scep",
		Name:      "flarex-ca",
		Subject:   profile.SubjectName([2]string{"O", "FlareX"}, [2]string{"CN", "%HardwareUUID%"}),
		Challenge: "one-time-challenge",
		Keysize:   2048,
		KeyType:   "RSA",
		KeyUsage:  profile.KeyUsageBoth,
	})

	mdm := profile.NewMDM(
		"io.flarex.mdm.enroll.mdm",
		"https://mdm.flarex.io/mdm",
		"https://mdm.flarex.io/checkin",
		"com.apple.mgmt.External.abc",
		scep, // identity payload; the binding is derived from this
	)

	// The crux of (C): the MDM payload must authenticate with the certificate the
	// SCEP payload provisions. The binding has to hold.
	assert.Equal(t, scep.UUID(), mdm.IdentityCertificateUUID)

	p := profile.New("io.flarex.mdm.enroll", "FlareX Enrollment")
	p.Add(scep, mdm)

	out, err := p.Marshal()
	require.NoError(t, err)

	xml := string(out)
	assert.Contains(t, xml, "com.apple.security.scep")
	assert.Contains(t, xml, "com.apple.mdm")
	assert.Contains(t, xml, "<key>IdentityCertificateUUID</key>")
	assert.Contains(t, xml, scep.UUID(), "the SCEP UUID must appear as the MDM identity binding")
	// Apple's quirky key spacing must survive marshalling verbatim.
	assert.Contains(t, xml, "<key>Key Type</key>")

	t.Log("\n" + xml) // eyeball the generated .mobileconfig
}
