package resourcelease149

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}
type Coordinator struct {
	core   *Table
	policy *Policy
	mu     sync.Mutex
	seq    uint64
	log    []Decision
}

func NewCoordinator(core *Table, policy *Policy) (*Coordinator, error) {
	if core == nil || policy == nil {
		return nil, ErrInvalidOptions
	}
	return &Coordinator{core: core, policy: policy}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	d := Decision{Sequence: c.seq, Actor: actor}
	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		d.Error = err.Error()
		c.log = append(c.log, d)
		return Result{}, err
	}
	r, err := c.core.Apply(b)
	if err != nil {
		d.Error = err.Error()
		c.log = append(c.log, d)
		return Result{}, err
	}
	d.Committed = true
	d.Generation = r.Generation
	c.log = append(c.log, d)
	return r, nil
}

func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.log))
	copy(out, c.log)
	return out
}
