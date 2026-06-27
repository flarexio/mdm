package inmem

import (
	"sync"

	"github.com/flarexio/core/events"

	"github.com/flarexio/mdm/enrollment"
)

// NewEnrollmentRepository returns an in-memory enrollment.Repository. A scaled
// deployment swaps in a database-backed implementation behind the same interface.
func NewEnrollmentRepository() (enrollment.Repository, error) {
	return &enrollmentRepository{
		enrollments: make(map[enrollment.ID]*enrollment.Enrollment),
		udids:       make(map[string]*enrollment.Enrollment),
	}, nil
}

type enrollmentRepository struct {
	sync.RWMutex
	enrollments map[enrollment.ID]*enrollment.Enrollment // by enrollment ID (cert CN)
	udids       map[string]*enrollment.Enrollment        // by device UDID
}

func (repo *enrollmentRepository) Store(e *enrollment.Enrollment) error {
	repo.Lock()
	defer repo.Unlock()

	// Persist a copy without the event store: events are an in-flight concern of
	// the aggregate, not stored state.
	stored := new(enrollment.Enrollment)
	*stored = *e
	stored.EventStore = nil

	repo.enrollments[stored.ID] = stored
	if stored.UDID != "" {
		repo.udids[stored.UDID] = stored
	}

	return nil
}

func (repo *enrollmentRepository) Delete(e *enrollment.Enrollment) error {
	repo.Lock()
	defer repo.Unlock()

	delete(repo.enrollments, e.ID)
	delete(repo.udids, e.UDID)

	return nil
}

func (repo *enrollmentRepository) ListAll() ([]*enrollment.Enrollment, error) {
	repo.RLock()
	defer repo.RUnlock()

	all := make([]*enrollment.Enrollment, 0, len(repo.enrollments))
	for _, e := range repo.enrollments {
		e.EventStore = events.NewEventStore()
		all = append(all, e)
	}

	return all, nil
}

func (repo *enrollmentRepository) Find(id enrollment.ID) (*enrollment.Enrollment, error) {
	repo.RLock()
	defer repo.RUnlock()

	e, ok := repo.enrollments[id]
	if !ok {
		return nil, enrollment.ErrEnrollmentNotFound
	}

	e.EventStore = events.NewEventStore()
	return e, nil
}

func (repo *enrollmentRepository) FindByUDID(udid string) (*enrollment.Enrollment, error) {
	repo.RLock()
	defer repo.RUnlock()

	e, ok := repo.udids[udid]
	if !ok {
		return nil, enrollment.ErrEnrollmentNotFound
	}

	e.EventStore = events.NewEventStore()
	return e, nil
}

func (repo *enrollmentRepository) Close() error {
	return nil
}
