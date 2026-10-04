package rangelock

import "errors"

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrInvalidTime    = errors.New("invalid time")
	ErrInvalidToken   = errors.New("invalid token")
	ErrExists         = errors.New("id exists")
	ErrConflict       = errors.New("lock conflict")
	ErrCapacity       = errors.New("capacity exceeded")
	ErrStaleToken     = errors.New("stale token")
)

type Mode uint8

const (
	Read Mode = iota + 1
	Write
)

type Options struct{ MaxLocks, MaxOwners, MaxMetadataBytes, MaxNameBytes int }
type Request struct {
	ID, Owner, Resource string
	Start, End          int64
	Mode                Mode
	TTL                 int64
	Metadata            []byte
}
type Lease struct {
	ID, Owner, Resource string
	Start, End          int64
	Mode                Mode
	Token               uint64
	ExpiresAt           int64
	Metadata            []byte
}
type Snapshot struct {
	Generation, NextToken        uint64
	Locks, Owners, MetadataBytes int
	Leases                       []Lease
}
type Manager struct{}

func New(opts Options) (*Manager, error) { return nil, ErrInvalidOptions }
func (m *Manager) AcquireBatch(now int64, requests []Request) ([]Lease, uint64, error) {
	return nil, 0, ErrInvalidInput
}
func (m *Manager) Renew(id string, token uint64, now, ttl int64) error           { return ErrStaleToken }
func (m *Manager) Release(id string, token uint64) error                         { return ErrStaleToken }
func (m *Manager) Sweep(now int64, limit int) ([]string, error)                  { return nil, nil }
func (m *Manager) Query(resource string, start, end, now int64) ([]Lease, error) { return nil, nil }
func (m *Manager) Snapshot() Snapshot                                            { return Snapshot{} }
