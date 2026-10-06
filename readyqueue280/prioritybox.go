package readyqueue280

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

// Queue is a concurrency-safe in-memory ready-priority queue.
//
// State is guarded by a single mutex. Items are indexed by ID in a map
// for O(1) lookup; Pop sorts the ready candidates at selection time.
type Queue struct {
	mu           sync.Mutex
	opts         Options
	items        map[string]Item
	generation   uint64
	nextRevision uint64
	now          int64
}

func New(opts Options) (*Queue, error) {
	if opts.MaxItems <= 0 || opts.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{opts: opts, items: make(map[string]Item), nextRevision: 1}, nil
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

// Apply atomically executes the batch's ops in order. On any failure the
// time, item state and revision counter are rolled back.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := validateBatchStructural(b, q.opts.MaxIDBytes); err != nil {
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
	// Candidate transaction: mutate a scratch copy, commit only on success.
	cand := make(map[string]Item, len(q.items)+len(b.Ops))
	for id, it := range q.items {
		cand[id] = it
	}
	nextRev := q.nextRevision
	var lastRev uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := cand[op.ID]; ok {
				return Result{}, ErrExists
			}
			cand[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRev}
			lastRev = nextRev
			nextRev++
		case Cancel:
			if _, ok := cand[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.ID)
		}
	}
	// Final capacity is checked only at the end.
	if len(cand) > q.opts.MaxItems {
		return Result{}, ErrCapacity
	}
	q.items = cand
	q.nextRevision = nextRev
	q.now = b.Now
	q.generation++
	return Result{Generation: q.generation, Revision: lastRev}, nil
}

// Pop atomically removes and returns up to limit ready items
// (ReadyAt <= now) in canonical order.
func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	if now < 0 || limit <= 0 {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if now < q.now {
		return nil, ErrTime
	}
	ready := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		if it.ReadyAt <= now {
			ready = append(ready, it)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return less(ready[i], ready[j]) })
	if len(ready) > limit {
		ready = ready[:limit]
	}
	for _, it := range ready {
		delete(q.items, it.ID)
	}
	q.now = now
	out := make([]Item, len(ready))
	copy(out, ready)
	return out, nil
}

// Snapshot returns an isolated copy of the current state.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return less(items[i], items[j]) })
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}
