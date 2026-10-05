package resourcelease134

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

type Coordinator struct {
	mu        sync.Mutex
	table     *Table
	policy    *Policy
	seq       uint64
	decisions []Decision
}

func NewCoordinator(t *Table, p *Policy) (*Coordinator, error) {
	if t == nil || p == nil {
		return nil, ErrInvalidOptions
	}
	return &Coordinator{table: t, policy: p}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	d := Decision{Sequence: c.seq, Actor: actor}
	fail := func(err error) (Result, error) {
		d.Error = err.Error()
		c.decisions = append(c.decisions, d)
		return Result{}, err
	}
	// Authorize before touching core state.
	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		return fail(err)
	}
	res, err := c.table.Apply(b)
	if err != nil {
		return fail(err)
	}
	d.Committed = true
	d.Generation = res.Generation
	c.decisions = append(c.decisions, d)
	return res, nil
}

func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.decisions))
	copy(out, c.decisions)
	return out
}
