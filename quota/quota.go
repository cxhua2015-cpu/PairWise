package quota

import "errors"

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrExists         = errors.New("already exists")
	ErrExceeded       = errors.New("quota exceeded")
	ErrCapacity       = errors.New("capacity exceeded")
	ErrBusy           = errors.New("subject busy")
)

type Options struct{ MaxSubjects, MaxReservations, MaxMetadataBytes, MaxNameBytes int }
type Limit struct {
	Subject, Dimension string
	Amount             int64
}
type Demand struct {
	Subject, Dimension string
	Amount             int64
}
type DimensionSnapshot struct {
	Dimension   string
	Limit, Used int64
}
type SubjectSnapshot struct {
	Subject    string
	Dimensions []DimensionSnapshot
}
type ReservationSnapshot struct {
	ID       string
	Demands  []Demand
	Metadata []byte
}
type Snapshot struct {
	Generation                            uint64
	Subjects, Reservations, MetadataBytes int
	SubjectState                          []SubjectSnapshot
	ReservationState                      []ReservationSnapshot
}
type Manager struct{}

func New(opts Options) (*Manager, error)                                      { return nil, ErrInvalidOptions }
func (m *Manager) SetLimits(limits []Limit) error                             { return ErrInvalidInput }
func (m *Manager) Reserve(id string, demands []Demand, metadata []byte) error { return ErrInvalidInput }
func (m *Manager) Replace(id string, demands []Demand, metadata []byte) error { return ErrInvalidInput }
func (m *Manager) Release(id string) error                                    { return ErrNotFound }
func (m *Manager) DeleteSubject(subject string) error                         { return ErrNotFound }
func (m *Manager) Snapshot() Snapshot                                         { return Snapshot{} }
