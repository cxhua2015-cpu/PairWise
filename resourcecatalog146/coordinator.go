package resourcecatalog146

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
	seq       uint64
	decisions []Decision
}

func NewCoordinator(store *Store, policy *Policy) (*Coordinator, error) {
	if store == nil || policy == nil {
		return nil, ErrInvalidInput
	}
	return &Coordinator{store: store, policy: policy}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		c.record(actor, false, 0, err)
		return Result{}, err
	}
	res, err := c.store.Apply(b)
	if err != nil {
		c.record(actor, false, 0, err)
		return Result{}, err
	}
	c.record(actor, true, res.Generation, nil)
	return res, nil
}

func (c *Coordinator) record(actor string, committed bool, generation uint64, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	d := Decision{Sequence: c.seq, Actor: actor, Committed: committed, Generation: generation}
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
