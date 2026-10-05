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
	mu        sync.Mutex
	table     *Table
	policy    *Policy
	nextSeq   uint64
	decisions []Decision
}

func NewCoordinator(t *Table, p *Policy) (*Coordinator, error) {
	if t == nil || p == nil {
		return nil, ErrInvalidInput
	}
	return &Coordinator{table: t, policy: p, nextSeq: 1}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	// Authorize first: policy rejection must not touch core state.
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

func (c *Coordinator) record(actor string, committed bool, generation uint64, err error) {
	d := Decision{Actor: actor, Committed: committed, Generation: generation}
	if err != nil {
		d.Error = err.Error()
	}
	c.mu.Lock()
	d.Sequence = c.nextSeq
	c.nextSeq++
	c.decisions = append(c.decisions, d)
	c.mu.Unlock()
}

func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.decisions))
	copy(out, c.decisions)
	return out
}
