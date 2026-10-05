package resourcelease149

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

// Coordinator serializes admission, delegates accepted batches to the
// state engine, and records a monotonic audit log of every attempt.
type Coordinator struct {
	mu     sync.Mutex
	table  *Table
	policy *Policy
	seq    uint64
	log    []Decision
}

func NewCoordinator(t *Table, p *Policy) (*Coordinator, error) {
	if t == nil || p == nil {
		return nil, ErrInvalidInput
	}
	return &Coordinator{table: t, policy: p}, nil
}

func (c *Coordinator) record(actor string, committed bool, generation uint64, err error) {
	c.seq++
	d := Decision{Sequence: c.seq, Actor: actor, Committed: committed, Generation: generation}
	if err != nil {
		d.Error = err.Error()
	}
	c.log = append(c.log, d)
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Authorization happens before the core state is read or mutated.
	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		c.record(actor, false, 0, err)
		return Result{}, err
	}
	res, err := c.table.Apply(b)
	if err != nil {
		c.record(actor, false, 0, err)
		return Result{}, err
	}
	c.record(actor, true, res.Generation, nil)
	return res, nil
}

// Decisions returns a copy of the audit log; callers cannot alias or
// mutate the coordinator's internal storage.
func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.log))
	copy(out, c.log)
	return out
}
