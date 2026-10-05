package resourcecatalog136

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

type Coordinator struct {
	store  *Store
	policy *Policy

	mu        sync.Mutex
	sequence  uint64
	decisions []Decision
}

func NewCoordinator(s *Store, p *Policy) (*Coordinator, error) {
	if s == nil || p == nil {
		return nil, ErrInvalidInput
	}
	return &Coordinator{store: s, policy: p}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	// Admission runs before the state engine is touched; a policy
	// rejection never reads or mutates core state.
	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		c.record(actor, false, 0, err)
		return Result{}, err
	}
	result, err := c.store.Apply(b)
	if err != nil {
		c.record(actor, false, 0, err)
		return Result{}, err
	}
	c.record(actor, true, result.Generation, nil)
	return result, nil
}

func (c *Coordinator) record(actor string, committed bool, generation uint64, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sequence++
	d := Decision{Sequence: c.sequence, Actor: actor, Committed: committed, Generation: generation}
	if err != nil {
		d.Error = err.Error()
	}
	c.decisions = append(c.decisions, d)
}

func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.decisions))
	copy(out, c.decisions)
	return out
}
