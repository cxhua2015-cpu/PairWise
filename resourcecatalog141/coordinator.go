package resourcecatalog141

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
	mu     sync.Mutex
	next   uint64
	log    []Decision
}

func NewCoordinator(s *Store, p *Policy) (*Coordinator, error) {
	if s == nil || p == nil {
		return nil, ErrInvalidInput
	}
	return &Coordinator{store: s, policy: p, next: 1}, nil
}

func (c *Coordinator) record(actor string, committed bool, generation uint64, err error) {
	d := Decision{Actor: actor, Committed: committed, Generation: generation}
	if err != nil {
		d.Error = err.Error()
	}
	c.mu.Lock()
	d.Sequence = c.next
	c.next++
	c.log = append(c.log, d)
	c.mu.Unlock()
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

func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.log))
	copy(out, c.log)
	return out
}
