package resourcelease139

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
	table     *Table
	policy    *Policy
	sequence  uint64
	decisions []Decision
}

func NewCoordinator(t *Table, p *Policy) (*Coordinator, error) {
	if t == nil || p == nil {
		return nil, ErrInvalidOptions
	}
	return &Coordinator{table: t, policy: p}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		c.record(actor, false, 0, err)
		return Result{}, err
	}
	result, err := c.table.Apply(b)
	if err != nil {
		c.record(actor, false, 0, err)
		return Result{}, err
	}
	c.record(actor, true, result.Generation, nil)
	return result, nil
}

func (c *Coordinator) record(actor string, committed bool, generation uint64, err error) {
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
