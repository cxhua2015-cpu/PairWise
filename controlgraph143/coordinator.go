package controlgraph143

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

type Coordinator struct {
	core   *Graph
	policy *Policy
	mu     sync.Mutex
	seq    uint64
	log    []Decision
}

func NewCoordinator(core *Graph, policy *Policy) (*Coordinator, error) {
	if core == nil || policy == nil {
		return nil, ErrInvalidOptions
	}
	return &Coordinator{core: core, policy: policy}, nil
}

// Apply authorizes first, then delegates to the state engine. Every attempt,
// accepted or rejected, is recorded with a monotonic sequence number.
func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		c.record(actor, false, 0, err)
		return Result{}, err
	}
	res, err := c.core.Apply(b)
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
	c.seq++
	d.Sequence = c.seq
	c.log = append(c.log, d)
	c.mu.Unlock()
}

// Decisions returns an isolated copy of the audit log.
func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.log))
	copy(out, c.log)
	return out
}
