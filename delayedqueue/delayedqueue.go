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
	opts       Options
	jobs       map[string]Job
	now        int64
	generation uint64
	revision   uint64
}

func New(o Options) (*Queue, error) {
	if o.MaxJobs <= 0 || o.MaxIDBytes <= 0 || o.MaxPayloadBytes <= 0 || o.MaxTotalPayloadBytes <= 0 || o.MaxPayloadBytes > o.MaxTotalPayloadBytes {
		return nil, ErrInvalidOptions
	}
	return &Queue{opts: o, jobs: make(map[string]Job)}, nil
}

func (q *Queue) validID(id string) bool {
	if id == "" || len(id) > q.opts.MaxIDBytes {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '/' || c == '-') {
			return false
		}
	}
	return true
}

func (q *Queue) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if !q.validID(op.ID) || op.Payload == nil || len(op.Payload) > q.opts.MaxPayloadBytes || op.ReadyAt < b.Now {
				return ErrInvalidInput
			}
		case Reschedule:
			if !q.validID(op.ID) || op.Payload != nil || op.ReadyAt < b.Now {
				return ErrInvalidInput
			}
		case Cancel:
			if !q.validID(op.ID) || op.Priority != 0 || op.ReadyAt != 0 || op.Payload != nil {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := q.validate(b); err != nil {
		return Result{}, err
	}
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	cand := make(map[string]Job, len(q.jobs)+len(b.Ops))
	for k, v := range q.jobs {
		cand[k] = v
	}
	rev := q.revision
	touched := make(map[string]struct{}, len(b.Ops))
	for _, op := range b.Ops {
		touched[op.ID] = struct{}{}
		switch op.Kind {
		case Enqueue:
			if _, ok := cand[op.ID]; ok {
				return Result{}, ErrConflict
			}
			rev++
			p := make([]byte, len(op.Payload))
			copy(p, op.Payload)
			cand[op.ID] = Job{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Payload: p, Revision: rev}
		case Reschedule:
			j, ok := cand[op.ID]
			if !ok {
				return Result{}, ErrNotFound
			}
			rev++
			j.Priority = op.Priority
			j.ReadyAt = op.ReadyAt
			j.Revision = rev
			cand[op.ID] = j
		case Cancel:
			if _, ok := cand[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.ID)
		}
	}
	if len(cand) > q.opts.MaxJobs {
		return Result{}, ErrCapacity
	}
	total := 0
	for _, j := range cand {
		total += len(j.Payload)
	}
	if total > q.opts.MaxTotalPayloadBytes {
		return Result{}, ErrCapacity
	}
	q.jobs = cand
	q.now = b.Now
	q.revision = rev
	if len(b.Ops) > 0 {
		q.generation++
	}
	ids := make([]string, 0, len(touched))
	for id := range touched {
		if _, ok := cand[id]; ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	changed := make([]Job, 0, len(ids))
	for _, id := range ids {
		changed = append(changed, cloneJob(cand[id]))
	}
	return Result{Generation: q.generation, Revision: q.revision, Changed: changed}, nil
}

func cloneJob(j Job) Job {
	p := make([]byte, len(j.Payload))
	copy(p, j.Payload)
	j.Payload = p
	return j
}

func lessReady(a, b Job) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

func (q *Queue) readyLocked(now int64) []Job {
	var out []Job
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
	q.mu.Lock()
	defer q.mu.Unlock()
	if !validLimit(limit) {
		return nil, ErrInvalidInput
	}
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
	q.mu.Lock()
	defer q.mu.Unlock()
	if !validLimit(limit) {
		return nil, ErrInvalidInput
	}
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
	ids := make([]string, 0, len(q.jobs))
	for id := range q.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	jobs := make([]Job, 0, len(ids))
	for _, id := range ids {
		jobs = append(jobs, cloneJob(q.jobs[id]))
	}
	return Snapshot{Generation: q.generation, NextRevision: q.revision + 1, Now: q.now, Jobs: jobs}
}
