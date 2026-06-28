// Package auth verifies bearer tokens issued by flarexio/identity (EdDSA over
// Ed25519) against its published JWKS, so the MDM admin endpoints can trust a
// caller's identity without sharing a secret.
package auth

import (
	"context"
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

var ErrNoToken = errors.New("no bearer token")

// Claims mirrors identity's session token: the standard registered claims plus
// the caller's roles.
type Claims struct {
	jwt.RegisteredClaims
	Roles []string `json:"roles"`
}

// Map is the projection handed to the OPA policy as input.claims.
func (c *Claims) Map() map[string]any {
	return map[string]any{
		"sub":   c.Subject,
		"roles": c.Roles,
	}
}

// Verifier validates a bearer token and returns its claims.
type Verifier interface {
	Verify(ctx context.Context, bearer string) (*Claims, error)
}
