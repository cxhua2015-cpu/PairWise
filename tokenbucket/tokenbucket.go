package tokenbucket

import "errors"

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("bucket not found")
	ErrTimeBackwards  = errors.New("time moved backwards")
	ErrInsufficient   = errors.New("insufficient tokens")
	ErrOverflow       = errors.New("capacity overflow")
)

type Kind uint8

const (
	Acquire Kind = iota + 1
	Refund
)

type Options struct{ MaxBuckets, MaxNameBytes int }
type BucketSpec struct {
	Name                                string
	Capacity, RefillTokens, RefillEvery int64
}
type Change struct {
	Kind       Kind
	Bucket     string
	Tokens, At int64
}
type BucketState struct {
	Name                                                   string
	Capacity, Tokens, LastRefill, LastObserved, NextRefill int64
}
type Snapshot struct {
	Generation uint64
	Buckets    []BucketState
}
type Registry struct{}

func New(Options, []BucketSpec) (*Registry, error)           { return nil, ErrInvalidOptions }
func (*Registry) ApplyBatch([]Change) (uint64, error)        { return 0, ErrInvalidInput }
func (*Registry) Inspect(string, int64) (BucketState, error) { return BucketState{}, ErrInvalidInput }
func (*Registry) Sweep(int64) (uint64, error)                { return 0, ErrInvalidInput }
func (*Registry) Snapshot() Snapshot                         { return Snapshot{} }
