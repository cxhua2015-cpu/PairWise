package controlgraph138

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

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	// Authorize before touching core state; policy rejection must not
	// read or mutate the graph.
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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sequence++
	d := Decision{
		Sequence:   c.sequence,
		Actor:      actor,
		Committed:  committed,
		Generation: generation,
	}
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
