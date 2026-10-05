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
	seq       uint64
	decisions []Decision
}

func NewCoordinator(s *Store, p *Policy) (*Coordinator, error) {
	if s == nil || p == nil {
		return nil, ErrInvalidInput
	}
	return &Coordinator{store: s, policy: p}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	// Authorization happens before touching core state.
	err := c.policy.Authorize(actor, len(b.Ops))
	var res Result
	if err == nil {
		res, err = c.store.Apply(b)
	}

	d := Decision{Actor: actor, Committed: err == nil}
	if err != nil {
		d.Error = err.Error()
	} else {
		d.Generation = res.Generation
	}
	c.mu.Lock()
	c.seq++
	d.Sequence = c.seq
	c.decisions = append(c.decisions, d)
	c.mu.Unlock()
	return res, err
}

func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.decisions))
	copy(out, c.decisions)
	return out
}
