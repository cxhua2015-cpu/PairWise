package taskqueue140

import "sync"

type Decision struct {
	Sequence   uint64
	Actor      string
	Committed  bool
	Generation uint64
	Error      string
}

// Coordinator serializes admission: it authorizes through the Policy
// first, delegates accepted batches to the state engine, and records a
// monotonic decision log entry for every accepted, denied or failed
// attempt.
type Coordinator struct {
	mu     sync.Mutex
	queue  *Queue
	policy *Policy
	next   uint64
	log    []Decision
}

func NewCoordinator(q *Queue, p *Policy) (*Coordinator, error) {
	if q == nil || p == nil {
		return nil, ErrInvalidInput
	}
	return &Coordinator{queue: q, policy: p, next: 1}, nil
}

func (c *Coordinator) record(actor string, committed bool, generation uint64, err error) {
	d := Decision{Sequence: c.next, Actor: actor, Committed: committed, Generation: generation}
	if err != nil {
		d.Error = err.Error()
	}
	c.next++
	c.log = append(c.log, d)
}

func (c *Coordinator) Apply(actor string, b Batch) (Result, error) {
	// Authorize before touching the engine; policy rejection must not
	// read or mutate core state.
	err := c.policy.Authorize(actor, len(b.Ops))
	var res Result
	if err == nil {
		res, err = c.queue.Apply(b)
	}
	c.mu.Lock()
	c.record(actor, err == nil, res.Generation, err)
	c.mu.Unlock()
	return res, err
}

// Decisions returns a copy of the audit log; the returned slice never
// aliases internal storage.
func (c *Coordinator) Decisions() []Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Decision(nil), c.log...)
}
