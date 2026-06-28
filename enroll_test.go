package mdm_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm"
)

// stubChallenger records the subject and returns a fixed challenge.
type stubChallenger struct{ subject string }

func (s *stubChallenger) GenerateChallenge(_ context.Context, subject string) (string, error) {
	s.subject = subject
	return "CHALLENGE-XYZ", nil
}

func TestEnrollerProfile(t *testing.T) {
	ch := &stubChallenger{}
	enr := mdm.NewEnroller(ch, mdm.EnrollConfig{
		Identifier:   "io.flarex.mdm.enroll",
		Organization: "FlareX",
		SCEPURL:      "https://ca.flarex.io/scep/mdm",
		CAName:       "mdm-scep",
		ServerURL:    "https://mdm.flarex.io/server",
		CheckInURL:   "https://mdm.flarex.io/checkin",
		RootCA:       []byte{0x30, 0x82, 0x01, 0x02}, // base64 -> MIIBAg==
	})

	out, err := enr.Profile(context.Background(), "alice")
	require.NoError(t, err)
	xml := string(out)

	assert.Equal(t, "alice", ch.subject, "challenge is bound to the subject")
	assert.Contains(t, xml, "CHALLENGE-XYZ")
	assert.Contains(t, xml, "com.apple.security.scep")
	assert.Contains(t, xml, "com.apple.security.root", "trust anchor embedded when RootCA is set")
	assert.Contains(t, xml, "MIIBAg==")
}

func TestEnrollerProfile_NoRootCA(t *testing.T) {
	enr := mdm.NewEnroller(&stubChallenger{}, mdm.EnrollConfig{
		Identifier:   "io.flarex.mdm.enroll",
		Organization: "FlareX",
	})

	out, err := enr.Profile(context.Background(), "alice")
	require.NoError(t, err)
	assert.NotContains(t, string(out), "com.apple.security.root", "no anchor when RootCA is unset")
}
