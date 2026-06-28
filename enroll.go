package mdm

import (
	"context"
	"fmt"

	"github.com/flarexio/mdm/identity"
	"github.com/flarexio/mdm/profile"
)

// EnrollConfig holds the static values an enrollment profile is built from.
type EnrollConfig struct {
	Identifier   string
	Organization string
	SCEPURL      string
	CAName       string
	ServerURL    string
	CheckInURL   string
	Topic        string // from the push certificate
	RootCA       []byte // DER of the trust anchor; embedded so the device trusts the MDM/SCEP TLS certs (optional)
}

// Enroller fetches a one-time SCEP challenge from identity and assembles the
// enrollment .mobileconfig.
type Enroller interface {
	Profile(ctx context.Context, subject string) ([]byte, error)
}

func NewEnroller(challenger identity.Challenger, cfg EnrollConfig) Enroller {
	return &enroller{challenger: challenger, cfg: cfg}
}

type enroller struct {
	challenger identity.Challenger
	cfg        EnrollConfig
}

// Profile returns the .mobileconfig bytes. subject is both the challenge binding
// and the certificate CN.
func (e *enroller) Profile(ctx context.Context, subject string) ([]byte, error) {
	challenge, err := e.challenger.GenerateChallenge(ctx, subject)
	if err != nil {
		return nil, fmt.Errorf("enroll: %w", err)
	}

	scep := profile.NewSCEP(e.cfg.Identifier+".scep", profile.SCEPContent{
		URL:       e.cfg.SCEPURL,
		Name:      e.cfg.CAName,
		Subject:   profile.SubjectName([2]string{"O", e.cfg.Organization}, [2]string{"CN", subject}),
		Challenge: challenge,
		Keysize:   2048,
		KeyType:   "RSA",
		KeyUsage:  profile.KeyUsageBoth,
	})

	mdmPayload := profile.NewMDM(
		e.cfg.Identifier+".mdm",
		e.cfg.ServerURL,
		e.cfg.CheckInURL,
		e.cfg.Topic,
		scep,
	)

	p := profile.New(e.cfg.Identifier, e.cfg.Organization+" Enrollment")

	// Install the trust anchor first so the device trusts the MDM/SCEP TLS certs
	// it then contacts, when those run on a private (non-public) CA.
	if len(e.cfg.RootCA) > 0 {
		ca := profile.NewCertificate(e.cfg.Identifier+".ca", "ca.crt", e.cfg.RootCA)
		p.Add(ca)
	}
	p.Add(scep, mdmPayload)

	return p.Marshal()
}
