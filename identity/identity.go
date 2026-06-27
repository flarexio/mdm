// Package identity is mdm-server's client to the flarexio/identity service: the
// source of one-time SCEP enrollment challenges.
package identity

import "context"

// Challenger mints a one-time SCEP challenge bound to a subject, which
// mdm-server embeds in an enrollment profile's SCEP payload.
type Challenger interface {
	GenerateChallenge(ctx context.Context, subject string) (challenge string, err error)
}
