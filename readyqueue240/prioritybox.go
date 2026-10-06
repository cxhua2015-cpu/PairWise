package readyqueue240

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrTime           = errors.New("time moved backwards")
	ErrExists         = errors.New("already exists")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Enqueue Kind = iota + 1
	Cancel
)

type Options struct{ MaxItems, MaxIDBytes int }
type Op struct {
	Kind     Kind
	ID       string
	Priority int
	ReadyAt  int64
}
type Batch struct {
	Now int64
	Ops []Op
}
type Item struct {
	ID       string
	Priority int
	ReadyAt  int64
	Revision uint64
}
type Result struct{ Generation, Revision uint64 }
type Snapshot struct {
	Generation, NextRevision uint64
	Now                      int64
	Items                    []Item
}

// Queue is a concurrency-safe in-memory ready-first priority queue.
// The zero value is not usable; construct it with New.
type Queue struct {
	mu           sync.Mutex
	opts         Options
	generation   uint64
	nextRevision uint64
	now          int64
	items        map[string]Item
}

// New validates options and returns an empty queue.
func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		opts:         o,
		nextRevision: 1,
		items:        make(map[string]Item),
	}, nil
}

// less orders items canonically: Priority desc, ReadyAt asc, ID asc.
func less(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

func sortedItems(items map[string]Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

// Apply structurally validates the whole batch, then executes its ops
// sequentially and atomically against a candidate state. On any failure the
// queue state, logical clock and revision counter are left untouched.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := validateBatchStructure(q.opts, b); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		return Result{Generation: q.generation, Revision: q.nextRevision - 1}, nil
	}
	candidate := make(map[string]Item, len(q.items)+len(b.Ops))
	for id, it := range q.items {
		candidate[id] = it
	}
	nextRevision := q.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := candidate[op.ID]; ok {
				return Result{}, ErrExists
			}
			candidate[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRevision}
			nextRevision++
		case Cancel:
			if _, ok := candidate[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.ID)
		}
	}
	if len(candidate) > q.opts.MaxItems {
		return Result{}, ErrCapacity
	}
	q.items = candidate
	q.nextRevision = nextRevision
	q.now = b.Now
	q.generation++
	return Result{Generation: q.generation, Revision: nextRevision - 1}, nil
}

// Pop atomically removes up to n ready items (ReadyAt <= now) in canonical
// order. It does not advance the queue's logical clock.
func (q *Queue) Pop(now int64, n int) ([]Item, error) {
	if now < 0 || n < 0 {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	ready := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		if it.ReadyAt <= now {
			ready = append(ready, it)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return less(ready[i], ready[j]) })
	if n > len(ready) {
		n = len(ready)
	}
	out := make([]Item, n)
	for i, it := range ready[:n] {
		delete(q.items, it.ID)
		out[i] = it
	}
	return out, nil
}

// Snapshot returns a consistent, fully detached view of the queue.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        sortedItems(q.items),
	}
}
