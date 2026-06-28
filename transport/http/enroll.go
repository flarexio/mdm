package http

import (
	"net/http"

	"github.com/flarexio/mdm"
)

// EnrollHandler issues an enrollment .mobileconfig for the authenticated caller.
// The subject is taken from the token (claims.sub), never the URL, so a user can
// only enroll themselves — it relies on Authorizator having populated the claims.
func EnrollHandler(enroller mdm.Enroller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok || claims.Subject == "" {
			http.Error(w, "no authenticated subject", http.StatusUnauthorized)
			return
		}
		subject := claims.Subject

		mobileconfig, err := enroller.Profile(r.Context(), subject)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/x-apple-aspen-config")
		w.Write(mobileconfig)
	}
}
