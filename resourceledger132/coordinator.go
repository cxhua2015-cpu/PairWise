package resourceledger132

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

type Coordinator struct {
	core   *Ledger
	policy *Policy
	mu     sync.Mutex
	seq    uint64
	log    []Decision
}

func NewCoordinator(core *Ledger, policy *Policy) (*Coordinator, error) {
	if core == nil || policy == nil {
		return nil, ErrInvalidOptions
	}
	return &Coordinator{core: core, policy: policy}, nil
}

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

func (c *Coordinator) record(actor string, committed bool, gen uint64, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	d := Decision{Sequence: c.seq, Actor: actor, Committed: committed, Generation: gen}
	if err != nil {
		d.Error = err.Error()
	}
	c.log = append(c.log, d)
}

func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.log))
	copy(out, c.log)
	return out
}
