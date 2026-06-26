package enrollment

type Repository interface {
	// Command

	Store(e *Enrollment) error
	Delete(e *Enrollment) error

	// Query

	ListAll() ([]*Enrollment, error)
	Find(id ID) (*Enrollment, error)
	FindByUDID(udid string) (*Enrollment, error)

	// Close the repository
	Close() error
}
