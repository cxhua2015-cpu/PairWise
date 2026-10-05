package resourceledger137

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

type Coordinator struct {
	ledger *Ledger
	policy *Policy

	mu        sync.Mutex
	sequence  uint64
	decisions []Decision
}

func NewCoordinator(l *Ledger, p *Policy) (*Coordinator, error) {
	if l == nil || p == nil {
		return nil, ErrInvalidOptions
	}
	return &Coordinator{ledger: l, policy: p}, nil
}

// Apply serializes admission: authorize first, then delegate to the engine,
// and record a monotonic decision for every attempt.
func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sequence++
	seq := c.sequence
	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		c.decisions = append(c.decisions, Decision{
			Sequence: seq, Actor: actor, Error: err.Error(),
		})
		return Result{}, err
	}
	res, err := c.ledger.Apply(b)
	d := Decision{Sequence: seq, Actor: actor}
	if err != nil {
		d.Error = err.Error()
	} else {
		d.Committed = true
		d.Generation = res.Generation
	}
	c.decisions = append(c.decisions, d)
	return res, err
}

// Decisions returns a copy of the audit log; callers cannot alias internal state.
func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.decisions))
	copy(out, c.decisions)
	return out
}
