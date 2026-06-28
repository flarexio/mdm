package auth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm/auth"
)

const (
	testIssuer   = "identity.flarex.io"
	testAudience = "mdm.flarex.io"
)

// jwksServer serves a single-key JWKS for pub, shaped exactly like identity's
// JWKHandler, and reports how many times it was hit.
func jwksServer(t *testing.T, pub ed25519.PublicKey) (*httptest.Server, *int) {
	t.Helper()

	hash := sha256.Sum256(pub)
	kid := base64.RawURLEncoding.EncodeToString(hash[:16])

	body, err := json.Marshal(map[string]any{
		"keys": []map[string]string{{
			"kty": "OKP",
			"crv": "Ed25519",
			"x":   base64.RawURLEncoding.EncodeToString(pub),
			"alg": "EdDSA",
			"use": "sig",
			"kid": kid,
		}},
	})
	require.NoError(t, err)

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	t.Cleanup(srv.Close)

	return srv, &hits
}

// sign builds a token the way identity does: EdDSA, no kid header, multi-audience.
func sign(t *testing.T, priv ed25519.PrivateKey, aud []string, exp time.Time) string {
	t.Helper()

	claims := auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testIssuer,
			Subject:   "alice",
			Audience:  aud,
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Roles: []string{"user"},
	}

	s, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(priv)
	require.NoError(t, err)
	return s
}

func TestJWKSVerifier(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	srv, hits := jwksServer(t, pub)
	v := auth.NewJWKSVerifier(srv.URL, testIssuer, testAudience, srv.Client())

	t.Run("valid token", func(t *testing.T) {
		token := sign(t, priv, []string{testIssuer, testAudience}, time.Now().Add(time.Hour))
		claims, err := v.Verify(context.Background(), token)
		require.NoError(t, err)
		assert.Equal(t, "alice", claims.Subject)
		assert.Equal(t, []string{"user"}, claims.Roles)
	})

	t.Run("keys are cached, not refetched per call", func(t *testing.T) {
		token := sign(t, priv, []string{testAudience}, time.Now().Add(time.Hour))
		_, err := v.Verify(context.Background(), token)
		require.NoError(t, err)
		assert.Equal(t, 1, *hits, "a warm cache must not hit the JWKS endpoint again")
	})

	t.Run("wrong audience is rejected", func(t *testing.T) {
		token := sign(t, priv, []string{"wallet.flarex.io"}, time.Now().Add(time.Hour))
		_, err := v.Verify(context.Background(), token)
		assert.Error(t, err)
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		token := sign(t, priv, []string{testAudience}, time.Now().Add(-time.Hour))
		_, err := v.Verify(context.Background(), token)
		assert.Error(t, err)
	})

	t.Run("token signed by a different key is rejected", func(t *testing.T) {
		_, other, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		token := sign(t, other, []string{testAudience}, time.Now().Add(time.Hour))
		_, err = v.Verify(context.Background(), token)
		assert.Error(t, err)
	})
}

func TestJWKSVerifier_EmptyToken(t *testing.T) {
	v := auth.NewJWKSVerifier("http://unused", testIssuer, testAudience, nil)
	_, err := v.Verify(context.Background(), "")
	assert.ErrorIs(t, err, auth.ErrNoToken)
}
