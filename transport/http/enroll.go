package http

import (
	"net/http"

	"github.com/flarexio/mdm"
)

// EnrollHandler issues an enrollment .mobileconfig for the subject in the path.
func EnrollHandler(enroller mdm.Enroller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subject := r.PathValue("subject")
		if subject == "" {
			http.Error(w, "subject required", http.StatusBadRequest)
			return
		}

		mobileconfig, err := enroller.Profile(r.Context(), subject)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/x-apple-aspen-config")
		w.Write(mobileconfig)
	}
}
