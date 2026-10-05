package taskqueue150

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
	queue     *Queue
	policy    *Policy
	decisions []Decision
}

func NewCoordinator(q *Queue, p *Policy) (*Coordinator, error) {
	if q == nil || p == nil {
		return nil, ErrInvalidInput
	}
	return &Coordinator{queue: q, policy: p}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := Decision{Sequence: uint64(len(c.decisions)) + 1, Actor: actor}
	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		d.Error = err.Error()
		c.decisions = append(c.decisions, d)
		return Result{}, err
	}
	res, err := c.queue.Apply(b)
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
