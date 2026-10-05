package controlgraph133

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

type Coordinator struct {
	graph  *Graph
	policy *Policy

	mu        sync.Mutex
	sequence  uint64
	decisions []Decision
}

func NewCoordinator(g *Graph, p *Policy) (*Coordinator, error) {
	if g == nil || p == nil {
		return nil, ErrInvalidInput
	}
	return &Coordinator{graph: g, policy: p}, nil
}

// Apply authorizes first, then delegates to the state engine. Every attempt
// — committed, denied, or failed in the engine — is appended to the audit
// log with the next contiguous sequence number.
func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		c.record(actor, false, 0, err)
		return Result{}, err
	}
	res, err := c.graph.Apply(b)
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
	c.sequence++
	d.Sequence = c.sequence
	c.decisions = append(c.decisions, d)
	c.mu.Unlock()
}

// Decisions returns a copy of the audit log; callers cannot alias or mutate
// internal storage.
func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.decisions))
	copy(out, c.decisions)
	return out
}
