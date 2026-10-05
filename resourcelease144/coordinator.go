package resourcelease144

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

type Coordinator struct {
	table  *Table
	policy *Policy

	mu    sync.Mutex
	next  uint64
	audit []Decision
}

func NewCoordinator(t *Table, p *Policy) (*Coordinator, error) {
	if t == nil || p == nil {
		return nil, ErrInvalidOptions
	}
	return &Coordinator{table: t, policy: p, next: 1}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	// Authorize before touching core state; policy rejection must not
	// read or mutate the table.
	err := c.policy.Authorize(actor, len(b.Ops))
	var res Result
	committed := false
	var gen uint64
	if err == nil {
		res, err = c.table.Apply(b)
		if err == nil {
			committed = true
			gen = res.Generation
		}
	}
	c.mu.Lock()
	seq := c.next
	c.next++
	d := Decision{Sequence: seq, Actor: actor, Committed: committed, Generation: gen}
	if err != nil {
		d.Error = err.Error()
	}
	c.audit = append(c.audit, d)
	c.mu.Unlock()
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.audit))
	copy(out, c.audit)
	return out
}
