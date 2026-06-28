// Package badger is a durable BadgerDB-backed implementation of the enrollment
// repository, so enrollments survive a restart (unlike persistence/inmem).
package badger

import (
	"encoding/json"
	"errors"

	badger "github.com/dgraph-io/badger/v4"

	"github.com/flarexio/core/events"

	"github.com/flarexio/mdm/enrollment"
)

const (
	enrollmentPrefix = "enrollment/"
	udidPrefix       = "udid/"
)

type enrollmentRepository struct {
	db *badger.DB
}

// NewEnrollmentRepository opens (or creates) a BadgerDB at dir.
func NewEnrollmentRepository(dir string) (enrollment.Repository, error) {
	opts := badger.DefaultOptions(dir).WithLoggingLevel(badger.ERROR)
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	return &enrollmentRepository{db}, nil
}

func enrollmentKey(id enrollment.ID) []byte { return []byte(enrollmentPrefix + id.String()) }
func udidKey(udid string) []byte            { return []byte(udidPrefix + udid) }

// Store persists the enrollment under its ID and maintains a UDID -> ID index.
// EventStore is dropped by the json:"-" tag, matching the inmem repository.
func (repo *enrollmentRepository) Store(e *enrollment.Enrollment) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}

	return repo.db.Update(func(txn *badger.Txn) error {
		if err := txn.Set(enrollmentKey(e.ID), data); err != nil {
			return err
		}
		if e.UDID != "" {
			return txn.Set(udidKey(e.UDID), []byte(e.ID))
		}
		return nil
	})
}

func (repo *enrollmentRepository) Delete(e *enrollment.Enrollment) error {
	return repo.db.Update(func(txn *badger.Txn) error {
		if err := txn.Delete(enrollmentKey(e.ID)); err != nil {
			return err
		}
		if e.UDID != "" {
			return txn.Delete(udidKey(e.UDID))
		}
		return nil
	})
}

func (repo *enrollmentRepository) Find(id enrollment.ID) (*enrollment.Enrollment, error) {
	var e *enrollment.Enrollment
	err := repo.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(enrollmentKey(id))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			e, err = unmarshal(val)
			return err
		})
	})

	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, enrollment.ErrEnrollmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (repo *enrollmentRepository) FindByUDID(udid string) (*enrollment.Enrollment, error) {
	var id enrollment.ID
	err := repo.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(udidKey(udid))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			id = enrollment.ID(val)
			return nil
		})
	})

	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, enrollment.ErrEnrollmentNotFound
	}
	if err != nil {
		return nil, err
	}
	return repo.Find(id)
}

func (repo *enrollmentRepository) ListAll() ([]*enrollment.Enrollment, error) {
	all := make([]*enrollment.Enrollment, 0)
	prefix := []byte(enrollmentPrefix)

	err := repo.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			err := it.Item().Value(func(val []byte) error {
				e, err := unmarshal(val)
				if err != nil {
					return err
				}
				all = append(all, e)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	return all, err
}

func (repo *enrollmentRepository) Close() error {
	return repo.db.Close()
}

func unmarshal(data []byte) (*enrollment.Enrollment, error) {
	var e enrollment.Enrollment
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	e.EventStore = events.NewEventStore()
	return &e, nil
}
