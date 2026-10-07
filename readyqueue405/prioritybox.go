package readyqueue405

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
// The zero value is not usable; construct it with New.
type Queue struct {
	mu           sync.Mutex
	maxItems     int
	maxIDBytes   int
	now          int64
	generation   uint64
	nextRevision uint64
	items        map[string]Item
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems:     o.MaxItems,
		maxIDBytes:   o.MaxIDBytes,
		nextRevision: 1,
		items:        make(map[string]Item),
	}, nil
}

// less orders items by Priority desc, ReadyAt asc, ID asc.
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

// Apply executes a batch atomically: ops run in order, capacity is
// checked only at the end, and any failure leaves state, time and
// revision counters untouched.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := q.ValidateBatch(b); err != nil {
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
	// Candidate transaction: mutate a copy, commit only on success.
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
	if len(candidate) > q.maxItems {
		return Result{}, ErrCapacity
	}
	q.items = candidate
	q.nextRevision = nextRevision
	q.now = b.Now
	q.generation++
	return Result{Generation: q.generation, Revision: nextRevision - 1}, nil
}

// Pop atomically removes and returns up to limit ready items in
// canonical order, advancing the logical clock.
func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	if now < 0 || limit <= 0 {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if now < q.now {
		return nil, ErrTime
	}
	var ready []Item
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
	return ready, nil
}

// Snapshot returns a consistent view; the returned slice is fully
// detached from internal state.
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
