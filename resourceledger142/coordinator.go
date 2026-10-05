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
	ledger *Ledger
	policy *Policy

	mu        sync.Mutex
	nextSeq   uint64
	decisions []Decision
}

func NewCoordinator(l *Ledger, p *Policy) (*Coordinator, error) {
	if l == nil || p == nil {
		return nil, ErrInvalidOptions
	}
	return &Coordinator{ledger: l, policy: p, nextSeq: 1}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	err := c.policy.Authorize(actor, len(b.Ops))
	var res Result
	committed := false
	var gen uint64
	if err == nil {
		res, err = c.ledger.Apply(b)
		if err == nil {
			committed = true
			gen = res.Generation
		}
	}
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	c.mu.Lock()
	c.decisions = append(c.decisions, Decision{
		Sequence:   c.nextSeq,
		Actor:      actor,
		Committed:  committed,
		Generation: gen,
		Error:      errStr,
	})
	c.nextSeq++
	c.mu.Unlock()
	return res, err
}

func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.decisions))
	copy(out, c.decisions)
	return out
}
