package resourcelease139

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

// Coordinator serializes admission: every attempt is authorized against
// the policy first, delegated to the state engine only when accepted,
// and appended to a monotonic decision log.
type Coordinator struct {
	mu        sync.Mutex
	table     *Table
	policy    *Policy
	nextSeq   uint64
	decisions []Decision
}

func NewCoordinator(t *Table, p *Policy) (*Coordinator, error) {
	if t == nil || p == nil {
		return nil, ErrInvalidInput
	}
	return &Coordinator{table: t, policy: p, nextSeq: 1}, nil
}

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

	res, err := c.table.Apply(b)
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
