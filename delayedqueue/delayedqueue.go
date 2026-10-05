package delayedqueue

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Options struct{ MaxJobs, MaxIDBytes, MaxPayloadBytes, MaxTotalPayloadBytes int }
type OpKind uint8

const (
	Enqueue OpKind = iota + 1
	Reschedule
	Cancel
)

type Op struct {
	Kind     OpKind
	ID       string
	Priority int
	ReadyAt  int64
	Payload  []byte
}
type Batch struct {
	Now int64
	Ops []Op
}
type Job struct {
	ID       string
	Priority int
	ReadyAt  int64
	Payload  []byte
	Revision uint64
}
type Result struct {
	Generation, Revision uint64
	Changed              []Job
}
type Snapshot struct {
	Generation, NextRevision uint64
	Now                      int64
	Jobs                     []Job
}

type Queue struct {
	mu         sync.Mutex
	opt        Options
	jobs       map[string]*Job
	now        int64
	generation uint64
	revision   uint64
}

func New(o Options) (*Queue, error) {
	if o.MaxJobs <= 0 || o.MaxIDBytes <= 0 || o.MaxPayloadBytes <= 0 ||
		o.MaxTotalPayloadBytes <= 0 || o.MaxPayloadBytes > o.MaxTotalPayloadBytes {
		return nil, ErrInvalidOptions
	}
	return &Queue{opt: o, jobs: make(map[string]*Job)}, nil
}

func validID(id string, max int) bool {
	if id == "" || len(id) > max {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '/' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func cloneJob(j *Job) Job {
	c := *j
	if j.Payload != nil {
		c.Payload = append([]byte(nil), j.Payload...)
	}
	return c
}

func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Phase 1: structural validation of every op, no state reads.
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if !validID(op.ID, q.opt.MaxIDBytes) || op.Payload == nil ||
				len(op.Payload) > q.opt.MaxPayloadBytes || op.ReadyAt < b.Now {
				return Result{}, ErrInvalidInput
			}
		case Reschedule:
			if !validID(op.ID, q.opt.MaxIDBytes) || op.Payload != nil || op.ReadyAt < b.Now {
				return Result{}, ErrInvalidInput
			}
		case Cancel:
			if !validID(op.ID, q.opt.MaxIDBytes) || op.Payload != nil ||
				op.Priority != 0 || op.ReadyAt != 0 {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
	}

	// Phase 2: global time monotonicity.
	if b.Now < q.now {
		return Result{}, ErrTime
	}

	// Phase 3: execute on an isolated candidate in input order.
	cand := make(map[string]*Job, len(q.jobs)+len(b.Ops))
	for id, j := range q.jobs {
		c := cloneJob(j)
		cand[id] = &c
	}
	rev := q.revision
	touched := make(map[string]struct{})
	var order []string
	touch := func(id string) {
		if _, ok := touched[id]; !ok {
			touched[id] = struct{}{}
			order = append(order, id)
		}
	}
	for _, op := range b.Ops {
		touch(op.ID)
		switch op.Kind {
		case Enqueue:
			if _, ok := cand[op.ID]; ok {
				return Result{}, ErrConflict
			}
			rev++
			cand[op.ID] = &Job{
				ID:       op.ID,
				Priority: op.Priority,
				ReadyAt:  op.ReadyAt,
				Payload:  append([]byte(nil), op.Payload...),
				Revision: rev,
			}
		case Reschedule:
			j, ok := cand[op.ID]
			if !ok {
				return Result{}, ErrNotFound
			}
			rev++
			j.Priority = op.Priority
			j.ReadyAt = op.ReadyAt
			j.Revision = rev
		case Cancel:
			if _, ok := cand[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.ID)
		}
	}

	// Phase 4: final capacity checks.
	if len(cand) > q.opt.MaxJobs {
		return Result{}, ErrCapacity
	}
	total := 0
	for _, j := range cand {
		total += len(j.Payload)
	}
	if total > q.opt.MaxTotalPayloadBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	q.jobs = cand
	q.now = b.Now
	q.revision = rev
	if len(b.Ops) > 0 {
		q.generation++
	}

	res := Result{Generation: q.generation, Revision: q.revision}
	ids := make([]string, 0, len(order))
	for _, id := range order {
		if _, ok := cand[id]; ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		res.Changed = append(res.Changed, cloneJob(cand[id]))
	}
	return res, nil
}

func lessReady(a, b *Job) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

func (q *Queue) readyLocked(now int64) []*Job {
	var out []*Job
	for _, j := range q.jobs {
		if j.ReadyAt <= now {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, k int) bool { return lessReady(out[i], out[k]) })
	return out
}

func validLimit(limit int) bool { return limit >= 1 && limit <= 1000 }

func (q *Queue) Peek(now int64, limit int) ([]Job, error) {
	if !validLimit(limit) {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if now < q.now {
		return nil, ErrTime
	}
	q.now = now
	ready := q.readyLocked(now)
	if len(ready) > limit {
		ready = ready[:limit]
	}
	out := make([]Job, 0, len(ready))
	for _, j := range ready {
		out = append(out, cloneJob(j))
	}
	return out, nil
}

func (q *Queue) Take(now int64, limit int) ([]Job, error) {
	if !validLimit(limit) {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if now < q.now {
		return nil, ErrTime
	}
	q.now = now
	ready := q.readyLocked(now)
	if len(ready) > limit {
		ready = ready[:limit]
	}
	out := make([]Job, 0, len(ready))
	for _, j := range ready {
		out = append(out, cloneJob(j))
		delete(q.jobs, j.ID)
	}
	if len(out) > 0 {
		q.generation++
	}
	return out, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := Snapshot{
		Generation:   q.generation,
		NextRevision: q.revision + 1,
		Now:          q.now,
	}
	ids := make([]string, 0, len(q.jobs))
	for id := range q.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		s.Jobs = append(s.Jobs, cloneJob(q.jobs[id]))
	}
	return s
}
