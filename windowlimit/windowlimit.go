package windowlimit

import "errors"

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct {
	Window                                int64
	Limit                                 uint64
	MaxKeys, MaxKeyBytes, MaxEventsPerKey int
}
type Request struct {
	Key   string
	Units uint64
}
type Batch struct {
	Now      int64
	Requests []Request
}
type Decision struct {
	Key             string
	Allowed         bool
	Used, Remaining uint64
	Revision        uint64
}
type Event struct {
	At              int64
	Units, Revision uint64
}
type KeyState struct {
	Key    string
	Events []Event
}
type Result struct {
	Generation, Revision uint64
	Decisions            []Decision
}
type Snapshot struct {
	Generation, NextRevision uint64
	Now                      int64
	Keys                     []KeyState
}
type Limiter struct{}

func New(Options) (*Limiter, error)          { return nil, ErrNotImplemented }
func (*Limiter) Check(Batch) (Result, error) { return Result{}, ErrNotImplemented }
func (*Limiter) Snapshot() Snapshot          { return Snapshot{} }
