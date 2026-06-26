package http

import (
	"io"
	"net/http"

	mdm "github.com/flarexio/mdm-server"
	"github.com/flarexio/mdm-server/command"
)

// mdmContentType is the content type Apple uses for MDM protocol plists.
const mdmContentType = "application/x-apple-aspen-mdm"

// CommandHandler serves the MDM ServerURL: the command/report loop. It expects
// RequireIdentity to have run first.
//
// The device POSTs either an Idle poll or the result of a previous command (both
// are Result plists). The handler records it and replies with the next command —
// or, when the queue is empty, an empty 200, which is how a device learns there is
// nothing more to do.
func CommandHandler(svc mdm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFromContext(r.Context())
		if !ok {
			http.Error(w, "no authenticated identity", http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}

		result, err := command.DecodeResult(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		next, err := svc.Command(id, result)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if next == nil {
			// Empty 200: no (more) commands. The device goes back to sleep.
			w.WriteHeader(http.StatusOK)
			return
		}

		w.Header().Set("Content-Type", mdmContentType)
		w.Write(next.Raw)
	}
}
