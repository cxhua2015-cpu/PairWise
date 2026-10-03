package leasegraph

import "errors"

const MaxTime int64 = 1_000_000_000_000_000

var (
	ErrNotImplemented    = errors.New("leasegraph: not implemented")
	ErrInvalidOptions    = errors.New("leasegraph: invalid options")
	ErrInvalidTime       = errors.New("leasegraph: invalid time")
	ErrInvalidID         = errors.New("leasegraph: invalid id")
	ErrInvalidPriority   = errors.New("leasegraph: invalid priority")
	ErrPayloadTooLarge   = errors.New("leasegraph: payload too large")
	ErrDuplicate         = errors.New("leasegraph: duplicate")
	ErrUnknownDependency = errors.New("leasegraph: unknown dependency")
	ErrCycle             = errors.New("leasegraph: dependency cycle")
	ErrCapacity          = errors.New("leasegraph: capacity exceeded")
	ErrNoReady           = errors.New("leasegraph: no ready task")
	ErrUnknownTask       = errors.New("leasegraph: unknown task")
	ErrNotRunning        = errors.New("leasegraph: task not running")
	ErrStaleLease        = errors.New("leasegraph: stale lease")
	ErrInvalidResult     = errors.New("leasegraph: invalid result")
)

type State string

const (
	StateBlocked   State = "blocked"
	StateReady     State = "ready"
	StateRunning   State = "running"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCanceled  State = "canceled"
)

type Options struct {
	MaxTasks      int
	MaxBytes      int
	LeaseDuration int64
	MaxAttempts   uint32
}

type TaskSpec struct {
	ID           string
	Dependencies []string
	Priority     int
	Payload      []byte
}

type Lease struct {
	ID       string
	Token    uint64
	Attempt  uint32
	Deadline int64
	Payload  []byte
}

type Transition struct {
	Ready    []string
	Failed   []string
	Canceled []string
}

type SweepResult struct {
	Expired  []string
	Ready    []string
	Failed   []string
	Canceled []string
}

type TaskView struct {
	ID           string
	Dependencies []string
	Priority     int
	State        State
	Attempt      uint32
	LeaseToken   uint64
	Deadline     int64
	Payload      []byte
	Result       []byte
}

type Snapshot struct {
	Tasks     []TaskView
	Ready     []string
	UsedBytes int
}

type Scheduler struct{}

func New(Options) (*Scheduler, error)         { return nil, ErrNotImplemented }
func (*Scheduler) AddBatch([]TaskSpec) error  { return ErrNotImplemented }
func (*Scheduler) Claim(int64) (Lease, error) { return Lease{}, ErrNotImplemented }
func (*Scheduler) Complete(int64, string, uint64, []byte, bool) (Transition, error) {
	return Transition{}, ErrNotImplemented
}
func (*Scheduler) Sweep(int64) (SweepResult, error) { return SweepResult{}, ErrNotImplemented }
func (*Scheduler) Snapshot() Snapshot               { return Snapshot{} }
