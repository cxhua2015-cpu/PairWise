package taskqueue150

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

// Coordinator serializes admission, delegates accepted batches to the
// state engine, and records a monotonic decision log for every attempt.
type Coordinator struct {
	mu        sync.Mutex
	queue     *Queue
	policy    *Policy
	seq       uint64
	decisions []Decision
}

func NewCoordinator(q *Queue, p *Policy) (*Coordinator, error) {
	if q == nil || p == nil {
		return nil, ErrInvalidInput
	}
	return &Coordinator{queue: q, policy: p}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	if actor == "" {
		return Result{}, ErrInvalidInput
	}
	// Authorize first; policy rejection must not read or mutate core state.
	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		c.record(actor, false, 0, err)
		return Result{}, err
	}
	res, err := c.queue.Apply(b)
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
	c.seq++
	d := Decision{Sequence: c.seq, Actor: actor, Committed: committed, Generation: generation}
	if err != nil {
		d.Error = err.Error()
	}
	c.decisions = append(c.decisions, d)
}

// Decisions returns a copy; the caller's slice never aliases internal storage.
func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.decisions))
	copy(out, c.decisions)
	return out
}
