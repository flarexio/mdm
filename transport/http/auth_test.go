package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/core/policy"

	"github.com/flarexio/mdm/auth"
	transhttp "github.com/flarexio/mdm/transport/http"
)

// roleVerifier maps a bearer token straight to that role's claims.
type roleVerifier struct{}

func (roleVerifier) Verify(_ context.Context, bearer string) (*auth.Claims, error) {
	if bearer == "" {
		return nil, errors.New("no token")
	}
	claims := &auth.Claims{Roles: []string{bearer}}
	claims.Subject = "tester"
	return claims, nil
}

const permissionsJSON = `{
  "role_permissions": {
    "admin": [ { "domain": "mdm::enrollments", "actions": ["list"] } ]
  },
  "who_enum": { "owner": 1, "group": 2, "others": 4, "admin": 8, "all": 16 }
}`

func testPolicy(t *testing.T) policy.Policy {
	t.Helper()
	path := filepath.Join(t.TempDir(), "permissions.json")
	require.NoError(t, os.WriteFile(path, []byte(permissionsJSON), 0o644))
	pol, err := policy.NewRegoPolicy(context.Background(), path)
	require.NoError(t, err)
	return pol
}

func TestAuthorizator(t *testing.T) {
	authz := transhttp.Authorizator(roleVerifier{}, testPolicy(t))
	h := authz("mdm::enrollments.list")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	do := func(authHeader string) int {
		req := httptest.NewRequest(http.MethodGet, "/enrollments", nil)
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	t.Run("admin role is allowed", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do("Bearer admin"))
	})

	t.Run("user role is forbidden", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, do("Bearer user"))
	})

	t.Run("missing token is unauthorized", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(""))
	})
}

func TestAuthorizator_DeniesUnknownAction(t *testing.T) {
	authz := transhttp.Authorizator(roleVerifier{}, testPolicy(t))
	h := authz("mdm::enrollments.remove")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodDelete, "/enrollments/x", nil)
	req.Header.Set("Authorization", "Bearer admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code, "admin lacks the remove action")
}
