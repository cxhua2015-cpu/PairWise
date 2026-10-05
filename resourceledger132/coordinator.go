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
	ledger *Ledger
	policy *Policy

	mu        sync.Mutex
	seq       uint64
	decisions []Decision
}

func NewCoordinator(l *Ledger, p *Policy) (*Coordinator, error) {
	if l == nil || p == nil {
		return nil, ErrInvalidOptions
	}
	return &Coordinator{ledger: l, policy: p}, nil
}

// Apply authorizes first, then delegates to the state engine. Every attempt
// (committed, denied, or engine failure) is appended to the audit log with a
// monotonic sequence number. Denied batches never touch core state.
func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	err := c.policy.Authorize(actor, len(b.Ops))
	var res Result
	if err == nil {
		res, err = c.ledger.Apply(b)
	}

	d := Decision{Actor: actor, Committed: err == nil}
	if err != nil {
		d.Error = err.Error()
	} else {
		d.Generation = res.Generation
	}
	c.mu.Lock()
	c.seq++
	d.Sequence = c.seq
	c.decisions = append(c.decisions, d)
	c.mu.Unlock()
	return res, err
}

// Decisions returns an isolated copy of the audit log.
func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.decisions))
	copy(out, c.decisions)
	return out
}
