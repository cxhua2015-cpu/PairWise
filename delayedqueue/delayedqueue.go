package delayedqueue

import "errors"

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct{ MaxJobs, MaxIDBytes, MaxPayloadBytes, MaxTotalPayloadBytes int }
type OpKind uint8

const (
	Enqueue OpKind = iota + 1
	Reschedule
	Cancel
)

type Op struct {
	Kind     OpKind
	ID       string
	Priority int
	ReadyAt  int64
	Payload  []byte
}
type Batch struct {
	Now int64
	Ops []Op
}
type Job struct {
	ID       string
	Priority int
	ReadyAt  int64
	Payload  []byte
	Revision uint64
}
type Result struct {
	Generation, Revision uint64
	Changed              []Job
}
type Snapshot struct {
	Generation, NextRevision uint64
	Now                      int64
	Jobs                     []Job
}
type Queue struct{}

func New(Options) (*Queue, error)             { return nil, ErrInvalidOptions }
func (*Queue) Apply(Batch) (Result, error)    { return Result{}, ErrInvalidInput }
func (*Queue) Peek(int64, int) ([]Job, error) { return nil, ErrInvalidInput }
func (*Queue) Take(int64, int) ([]Job, error) { return nil, ErrInvalidInput }
func (*Queue) Snapshot() Snapshot             { return Snapshot{} }
