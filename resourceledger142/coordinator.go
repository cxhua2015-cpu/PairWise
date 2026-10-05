package resourceledger142

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
	ledger    *Ledger
	policy    *Policy
	decisions []Decision
	nextSeq   uint64
}

func NewCoordinator(l *Ledger, p *Policy) (*Coordinator, error) {
	if l == nil || p == nil {
		return nil, ErrInvalidOptions
	}
	return &Coordinator{ledger: l, policy: p, nextSeq: 1}, nil
}

// Apply serializes admission: authorize first, then delegate to the engine,
// then append an audit decision. Policy denial never touches core state.
func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	d := Decision{Sequence: c.nextSeq, Actor: actor}
	c.nextSeq++

	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		d.Error = err.Error()
		c.decisions = append(c.decisions, d)
		return Result{}, err
	}
	res, err := c.ledger.Apply(b)
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

// Decisions returns a copy; callers cannot alias internal storage.
func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.decisions))
	copy(out, c.decisions)
	return out
}
