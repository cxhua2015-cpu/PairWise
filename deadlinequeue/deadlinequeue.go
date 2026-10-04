package deadlinequeue

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
	Add Kind = iota + 1
	Delete
)

type Options struct{ MaxTasks, MaxPayloadBytes, MaxNameBytes int }
type Task struct {
	ID, Queue string
	Due       int64
	Priority  int32
	Payload   []byte
}
type Change struct {
	Kind Kind
	Task Task
}
type Snapshot struct {
	Generation          uint64
	Tasks, PayloadBytes int
	Items               []Task
}
type Queue struct{}

func New(opts Options) (*Queue, error)                                            { return nil, ErrInvalidOptions }
func (q *Queue) ApplyBatch(changes []Change) (uint64, error)                      { return 0, ErrInvalidInput }
func (q *Queue) PopDue(queue string, now int64, limit int) ([]Task, error)        { return nil, nil }
func (q *Queue) Window(queue string, start, end int64, limit int) ([]Task, error) { return nil, nil }
func (q *Queue) Snapshot() Snapshot                                               { return Snapshot{} }
