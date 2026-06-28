package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/flarexio/mdm/auth"
)

// RequireToken authenticates an admin request with a bearer token verified by v
// (an identity-issued EdDSA JWT, checked against identity's JWKS). On success the
// caller's claims are stored in the context; otherwise the request is rejected
// with 401 before reaching the handler.
func RequireToken(v auth.Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok {
				http.Error(w, "bearer token required", http.StatusUnauthorized)
				return
			}

			claims, err := v.Verify(r.Context(), bearer)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromContext returns the caller's claims stored by RequireToken.
func ClaimsFromContext(ctx context.Context) (*auth.Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*auth.Claims)
	return claims, ok
}
