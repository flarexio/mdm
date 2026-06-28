package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/flarexio/mdm"
	"github.com/flarexio/mdm/enrollment"
)

// enrollmentView is the read model returned by the query endpoints: a
// demo/ops-friendly projection that renders Status as a string, exposes whether
// the device is pushable, and omits the raw push token.
type enrollmentView struct {
	ID        string    `json:"id"`
	UDID      string    `json:"udid"`
	Status    string    `json:"status"`
	CanPush   bool      `json:"can_push"`
	Topic     string    `json:"topic,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func newEnrollmentView(e *enrollment.Enrollment) enrollmentView {
	return enrollmentView{
		ID:        e.ID.String(),
		UDID:      e.UDID,
		Status:    e.Status.String(),
		CanPush:   e.CanPush(),
		Topic:     e.Push.Topic,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

// EnrollmentsHandler lists every enrollment the server knows about.
func EnrollmentsHandler(svc mdm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all, err := svc.Enrollments()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		views := make([]enrollmentView, 0, len(all))
		for _, e := range all {
			views = append(views, newEnrollmentView(e))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(views)
	}
}

// EnrollmentHandler returns a single enrollment by its subject (enrollment ID).
func EnrollmentHandler(svc mdm.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subject := r.PathValue("subject")
		if subject == "" {
			http.Error(w, "subject required", http.StatusBadRequest)
			return
		}

		e, err := svc.Enrollment(enrollment.ID(subject))
		if err != nil {
			if errors.Is(err, enrollment.ErrEnrollmentNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(newEnrollmentView(e))
	}
}
