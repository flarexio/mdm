package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/flarexio/mdm"
	"github.com/flarexio/mdm/command"
	"github.com/flarexio/mdm/enrollment"
)

// enqueueRequest is the admin command-enqueue body: a RequestType plus optional
// type-specific fields nested under command.
type enqueueRequest struct {
	RequestType command.RequestType `json:"requestType"`
	Command     map[string]any      `json:"command"`
}

// EnqueueHandler queues a command for the subject and wakes the device via APNs.
// It is an admin/integration entrypoint, separate from the device-facing mTLS
// channels: the subject in the path is the enrollment ID (the device
// certificate's CN), supplied by the operator rather than read from a client cert.
//
// Only commands registered in reg can be invoked; an unknown RequestType is
// rejected with a 400 before ever reaching the queue.
func EnqueueHandler(svc mdm.Service, reg *command.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subject := r.PathValue("subject")
		if subject == "" {
			http.Error(w, "subject required", http.StatusBadRequest)
			return
		}

		var req enqueueRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		cmd, err := reg.Build(req.RequestType, req.Command)
		if err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, command.ErrUnknownCommand) {
				code = http.StatusBadRequest
			}
			http.Error(w, err.Error(), code)
			return
		}

		if err := svc.Enqueue(enrollment.ID(subject), cmd); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{"commandUUID": cmd.CommandUUID})
	}
}
