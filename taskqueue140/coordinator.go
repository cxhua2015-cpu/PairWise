package taskqueue140

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

// Coordinator serializes admission: it authorizes each batch against the
// Policy before delegating to the state engine, and appends a monotonic
// audit decision for every accepted, denied or failed attempt.
type Coordinator struct {
	mu     sync.Mutex
	queue  *Queue
	policy *Policy
	seq    uint64
	log    []Decision
}

func NewCoordinator(q *Queue, p *Policy) (*Coordinator, error) {
	if q == nil || p == nil {
		return nil, ErrInvalidInput
	}
	return &Coordinator{queue: q, policy: p}, nil
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.seq++
	d := Decision{Sequence: c.seq, Actor: actor}

	// Authorize first; policy rejection must not read or mutate core state.
	if err := c.policy.Authorize(actor, len(b.Ops)); err != nil {
		d.Error = err.Error()
		c.log = append(c.log, d)
		return Result{}, err
	}

	res, err := c.queue.Apply(b)
	if err != nil {
		d.Error = err.Error()
		c.log = append(c.log, d)
		return Result{}, err
	}
	d.Committed = true
	d.Generation = res.Generation
	c.log = append(c.log, d)
	return res, nil
}

// Decisions returns a copy of the audit log; the returned slice does not
// alias internal storage.
func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Decision, len(c.log))
	copy(out, c.log)
	return out
}
