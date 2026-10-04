package leasepool

import "errors"

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct { MaxPools, MaxLeases, MaxNameBytes, MaxOwnerBytes int }
type PoolConfig struct { Name string; Capacity uint64 }
type OpKind uint8
const ( Acquire OpKind = iota + 1; Release; Renew )
type Op struct { Kind OpKind; Pool, LeaseID, Owner string; Weight uint64; ExpiresAt int64 }
type Batch struct { Now int64; Ops []Op }
type Result struct { Generation uint64; Expired []string }
type Lease struct { Pool, LeaseID, Owner string; Weight uint64; ExpiresAt int64 }
type PoolState struct { Name string; Capacity, Used uint64; Leases int }
type Snapshot struct { Generation uint64; Now int64; Pools []PoolState; Leases []Lease }
type Registry struct{}

func New(Options, []PoolConfig) (*Registry, error) { return nil, ErrInvalidOptions }
func (*Registry) Apply(Batch) (Result, error)       { return Result{}, ErrInvalidInput }
func (*Registry) Sweep(int64) ([]string, error)    { return nil, ErrInvalidInput }
func (*Registry) Snapshot() Snapshot               { return Snapshot{} }
