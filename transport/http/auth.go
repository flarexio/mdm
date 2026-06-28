package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/flarexio/core/policy"

	"github.com/flarexio/mdm/auth"
)

// Who selects, per route, which principals a permission applies to. It mirrors
// the flarexio authorizator's flags and is OR'd into input.who_flags for the OPA
// policy. Passing none means "any role that holds the permission".
type Who byte

const (
	Owner Who = 1 << iota
	Group
	Others
	Admin
	All
)

// Authz produces per-route authorization middleware for a "domain.action" rule.
type Authz func(rule string, who ...Who) func(http.Handler) http.Handler

// Authorizator authenticates the bearer token with v and authorizes the request
// against the OPA policy for the route's domain/action. It is the net/http
// counterpart of the flarexio gin authorizator: verify the identity-issued token,
// then Eval {domain, action, who_flags, claims} and 403 on deny.
func Authorizator(v auth.Verifier, pol policy.Policy) Authz {
	return func(rule string, who ...Who) func(http.Handler) http.Handler {
		domain, action, _ := strings.Cut(rule, ".")

		var flags byte
		for _, w := range who {
			flags |= byte(w)
		}

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

				input := map[string]any{
					"domain":    domain,
					"action":    action,
					"who_flags": flags,
					"claims":    claims.Map(),
				}
				if subject := r.PathValue("subject"); subject != "" {
					input["object"] = subject
				}

				allowed, err := pol.Eval(r.Context(), input)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				if !allowed {
					http.Error(w, "access denied", http.StatusForbidden)
					return
				}

				ctx := context.WithValue(r.Context(), claimsContextKey, claims)
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		}
	}
}

// ClaimsFromContext returns the caller's claims stored by Authorizator.
func ClaimsFromContext(ctx context.Context) (*auth.Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*auth.Claims)
	return claims, ok
}
