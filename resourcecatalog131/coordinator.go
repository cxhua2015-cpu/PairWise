package resourcecatalog131

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
	store     *Store
	policy    *Policy
	sequence  uint64
	decisions []Decision
}

func NewCoordinator(store *Store, policy *Policy) (*Coordinator, error) {
	if store == nil || policy == nil {
		return nil, ErrInvalidOptions
	}
	return &Coordinator{store: store, policy: policy}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sequence++
	d := Decision{Sequence: c.sequence, Actor: actor}

	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		d.Error = err.Error()
		c.decisions = append(c.decisions, d)
		return Result{}, err
	}
	res, err := c.store.Apply(b)
	if err != nil {
		d.Error = err.Error()
		c.decisions = append(c.decisions, d)
		return Result{}, err
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
