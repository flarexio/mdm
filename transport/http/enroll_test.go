package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flarexio/core/policy"

	transhttp "github.com/flarexio/mdm/transport/http"
)

// stubEnroller records the subject it was asked to issue a profile for.
type stubEnroller struct{ gotSubject string }

func (s *stubEnroller) Profile(_ context.Context, subject string) ([]byte, error) {
	s.gotSubject = subject
	return []byte("PROFILE"), nil
}

func enrollPolicy(t *testing.T) policy.Policy {
	t.Helper()
	const perms = `{
  "role_permissions": { "user": [ { "domain": "mdm::enroll", "actions": ["issue"] } ] },
  "who_enum": { "owner": 1, "group": 2, "others": 4, "admin": 8, "all": 16 }
}`
	path := filepath.Join(t.TempDir(), "permissions.json")
	require.NoError(t, os.WriteFile(path, []byte(perms), 0o644))
	pol, err := policy.NewRegoPolicy(context.Background(), path)
	require.NoError(t, err)
	return pol
}

func TestEnrollHandler_SubjectFromToken(t *testing.T) {
	enr := &stubEnroller{}
	authz := transhttp.Authorizator(roleVerifier{}, enrollPolicy(t))
	h := authz("mdm::enroll.issue")(transhttp.EnrollHandler(enr))

	// roleVerifier maps the bearer to that role and sets Subject = "tester".
	req := httptest.NewRequest(http.MethodPost, "/enroll", nil)
	req.Header.Set("Authorization", "Bearer user")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "tester", enr.gotSubject, "subject must come from the token, not the URL")
	assert.Equal(t, "application/x-apple-aspen-config", rec.Header().Get("Content-Type"))
}

func TestEnrollHandler_ForbiddenWithoutPermission(t *testing.T) {
	enr := &stubEnroller{}
	authz := transhttp.Authorizator(roleVerifier{}, enrollPolicy(t))
	h := authz("mdm::enroll.issue")(transhttp.EnrollHandler(enr))

	// "admin" role is not granted enroll in this policy → 403, enroller untouched.
	req := httptest.NewRequest(http.MethodPost, "/enroll", nil)
	req.Header.Set("Authorization", "Bearer admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, enr.gotSubject)
}
