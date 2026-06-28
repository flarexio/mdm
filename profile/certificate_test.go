package profile_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm/profile"
)

func TestCertificatePayload(t *testing.T) {
	der := []byte{0x30, 0x82, 0x01, 0x02} // arbitrary bytes; marshalled as <data>

	p := profile.New("io.flarex.mdm.enroll", "Test")
	p.Add(profile.NewCertificate("io.flarex.mdm.enroll.ca", "ca.crt", der))

	out, err := p.Marshal()
	require.NoError(t, err)
	xml := string(out)

	assert.Contains(t, xml, "com.apple.security.root")
	assert.Contains(t, xml, "ca.crt")
	// 0x30,0x82,0x01,0x02 base64-encodes to "MIIBAg=="
	assert.Contains(t, xml, "MIIBAg==")
	assert.True(t, strings.Contains(xml, "<data>"), "DER must marshal as a plist <data> element")
}
