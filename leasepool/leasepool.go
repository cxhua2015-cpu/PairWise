package leasepool

import "errors"

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrExists         = errors.New("already exists")
	ErrNotFound       = errors.New("not found")
	ErrBusy           = errors.New("busy")
	ErrOwner          = errors.New("owner mismatch")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Add Kind = iota + 1
	Remove
	Acquire
	Renew
	Release
)

type Options struct{ MaxResources, MaxNameBytes, MaxOwnerBytes int }
type Op struct {
	Kind            Kind
	Resource, Owner string
	ExpiresAt       int64
}
type Batch struct {
	Now int64
	Ops []Op
}
type Lease struct {
	Resource, Owner string
	ExpiresAt       int64
	Revision        uint64
}
type Result struct {
	Generation, Revision uint64
	Changed              []Lease
}
type Snapshot struct {
	Generation, NextRevision uint64
	Now                      int64
	Resources                []string
	Leases                   []Lease
}
type Pool struct{}

func New(Options) (*Pool, error)            { return nil, ErrNotImplemented }
func (*Pool) Apply(Batch) (Result, error)   { return Result{}, ErrNotImplemented }
func (*Pool) Expire(int64) ([]Lease, error) { return nil, ErrNotImplemented }
func (*Pool) Snapshot() Snapshot            { return Snapshot{} }
