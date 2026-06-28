package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/mdm/auth"
	transhttp "github.com/flarexio/mdm/transport/http"
)

// stubVerifier accepts exactly one token value.
type stubVerifier struct{ ok string }

func (s stubVerifier) Verify(_ context.Context, bearer string) (*auth.Claims, error) {
	if bearer != s.ok {
		return nil, errors.New("invalid token")
	}
	claims := &auth.Claims{Roles: []string{"user"}}
	claims.Subject = "alice"
	return claims, nil
}

func TestRequireToken(t *testing.T) {
	var seen *auth.Claims
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = transhttp.ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := transhttp.RequireToken(stubVerifier{ok: "good"})(next)

	t.Run("valid bearer passes and exposes claims", func(t *testing.T) {
		seen = nil
		req := httptest.NewRequest(http.MethodPost, "/enqueue/x", nil)
		req.Header.Set("Authorization", "Bearer good")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.NotNil(t, seen)
		assert.Equal(t, "alice", seen.Subject)
	})

	t.Run("missing header is 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/enqueue/x", nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("bad token is 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/enqueue/x", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}
