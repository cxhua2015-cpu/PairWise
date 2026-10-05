package resourcelease134

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

// Coordinator serializes admission: it authorizes against the Policy
// before delegating to the state engine, and appends a monotonically
// sequenced audit decision for every accepted or rejected attempt.
type Coordinator struct {
	mu       sync.Mutex
	table    *Table
	policy   *Policy
	seq      uint64
	decision []Decision
}

func NewCoordinator(t *Table, p *Policy) (*Coordinator, error) {
	if t == nil || p == nil {
		return nil, ErrInvalidInput
	}
	return &Coordinator{table: t, policy: p}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := Decision{Sequence: c.seq + 1, Actor: actor}
	// Authorize first; policy rejection must not read or mutate core state.
	var res Result
	err := c.policy.Authorize(actor, len(b.Ops))
	if err == nil {
		res, err = c.table.Apply(b)
	}
	if err != nil {
		d.Error = err.Error()
	} else {
		d.Committed = true
		d.Generation = res.Generation
	}
	c.seq = d.Sequence
	c.decision = append(c.decision, d)
	return res, err
}

func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.decision))
	copy(out, c.decision)
	return out
}
