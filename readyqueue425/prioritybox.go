package readyqueue425

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
	opts         Options
	items        map[string]Item
	now          int64
	generation   uint64
	nextRevision uint64
}

// New creates an empty Queue. Capacity and length limits must be positive.
func New(opts Options) (*Queue, error) {
	if opts.MaxItems <= 0 || opts.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		opts:         opts,
		items:        make(map[string]Item),
		nextRevision: 1,
	}, nil
}

// Apply validates the batch, then atomically executes its ops in order.
// Enqueue assigns the next revision; capacity is checked only at the end.
// Any failure rolls back time, state and revision.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := q.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	items := make(map[string]Item, len(q.items)+len(b.Ops))
	for id, it := range q.items {
		items[id] = it
	}
	nextRevision := q.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := items[op.ID]; ok {
				return Result{}, ErrExists
			}
			items[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRevision}
			nextRevision++
		case Cancel:
			if _, ok := items[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(items, op.ID)
		}
	}
	if len(items) > q.opts.MaxItems {
		return Result{}, ErrCapacity
	}
	q.items = items
	q.now = b.Now
	q.nextRevision = nextRevision
	if len(b.Ops) > 0 {
		q.generation++
	}
	var revision uint64
	if nextRevision > 1 {
		revision = nextRevision - 1
	}
	return Result{Generation: q.generation, Revision: revision}, nil
}

// Pop atomically removes and returns up to limit items with ReadyAt <= now,
// ordered by Priority descending, ReadyAt ascending, ID ascending.
func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	if now < 0 || limit < 0 {
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
	sortItems(ready)
	if len(ready) > limit {
		ready = ready[:limit]
	}
	for _, it := range ready {
		delete(q.items, it.ID)
	}
	q.now = now
	return ready, nil
}

// Snapshot returns a consistent copy of the current state. The returned
// slice is fully isolated from the queue's internal state.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.snapshotLocked()
}

func (q *Queue) snapshotLocked() Snapshot {
	items := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		items = append(items, it)
	}
	sortItems(items)
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}

// sortItems orders items canonically: Priority descending, ReadyAt
// ascending, ID ascending.
func sortItems(items []Item) {
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.ReadyAt != b.ReadyAt {
			return a.ReadyAt < b.ReadyAt
		}
		return a.ID < b.ID
	})
}
