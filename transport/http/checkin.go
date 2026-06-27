package http

import (
	"io"
	"net/http"

	"github.com/flarexio/mdm"
	"github.com/flarexio/mdm/checkin"
)

// CheckInHandler serves the MDM CheckInURL. It expects RequireIdentity to have run
// first, so the authenticated enrollment identity is already in the context — the
// handler never derives identity from the request body.
//
// A successful check-in returns 200 with an empty body, which is what Apple devices
// expect for Authenticate/TokenUpdate/CheckOut.
func CheckInHandler(svc mdm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFromContext(r.Context())
		if !ok {
			// Should be impossible behind RequireIdentity; fail closed if misconfigured.
			http.Error(w, "no authenticated identity", http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}

		msg, err := checkin.DecodeCheckin(body)
		if err != nil {
			// Malformed or an unmodelled MessageType.
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if err := svc.CheckIn(id, msg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}
