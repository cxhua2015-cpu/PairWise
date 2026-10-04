package fairqueue

import "errors"

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrExists         = errors.New("task exists")
	ErrNotFound       = errors.New("task not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Put Kind = iota + 1
	Delete
)

type Options struct{ MaxTasks, MaxPayloadBytes, MaxNameBytes int }
type QueueWeight struct {
	Queue  string
	Weight int
}
type Task struct {
	ID, Queue string
	Sequence  uint64
	Payload   []byte
}
type Change struct {
	Kind      Kind
	ID, Queue string
	Payload   []byte
}
type ScheduleResult struct {
	Generation uint64
	Cursor     int
	Tasks      []Task
}
type Snapshot struct {
	Generation          uint64
	NextSequence        uint64
	Cursor              int
	Tasks, PayloadBytes int
	Wheel               []string
	Items               []Task
}
type Queue struct{}

func New(Options, []QueueWeight) (*Queue, error)   { return nil, ErrInvalidOptions }
func (*Queue) ApplyBatch([]Change) (uint64, error) { return 0, ErrInvalidInput }
func (*Queue) Peek(int) (ScheduleResult, error)    { return ScheduleResult{}, nil }
func (*Queue) Dequeue(int) (ScheduleResult, error) { return ScheduleResult{}, nil }
func (*Queue) Snapshot() Snapshot                  { return Snapshot{} }
