package controlgraph148

import (
	"errors"
	"sync"
)

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

// Coordinator serializes admission, delegates to the state engine and
// records a monotonic audit log of every attempt.
type Coordinator struct {
	graph  *Graph
	policy *Policy

	mu        sync.Mutex
	sequence  uint64
	decisions []Decision
}

func NewCoordinator(g *Graph, p *Policy) (*Coordinator, error) {
	if g == nil || p == nil {
		return nil, errors.New("nil dependency")
	}
	return &Coordinator{graph: g, policy: p}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	// Authorize first: policy rejection must not read or mutate core state.
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

// Decisions returns a copy of the audit log; callers cannot alias internals.
func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.decisions))
	copy(out, c.decisions)
	return out
}
