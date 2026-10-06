package readyqueue285

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
// Ownership: a Queue owns every Item stored in items; Items are copied in
// and out of the structure so callers never alias internal state.
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

// lessCanonical orders by Priority desc, ReadyAt asc, ID asc.
func lessCanonical(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

// lastRevision returns the most recently assigned revision (0 if none).
// Caller must hold q.mu.
func (q *Queue) lastRevision() uint64 { return q.nextRevision - 1 }

// Apply executes the batch's ops atomically and in order. Structural
// validation runs before any state is read; the capacity limit is only
// enforced at the end. Any failure rolls back time, items and revisions.
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
		return Result{Generation: q.generation, Revision: q.lastRevision()}, nil
	}
	type undoEntry struct {
		id   string
		prev Item
		had  bool
	}
	undos := make([]undoEntry, 0, len(b.Ops))
	assigned := uint64(0)
	rollback := func(err error) (Result, error) {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.had {
				q.items[u.id] = u.prev
			} else {
				delete(q.items, u.id)
			}
		}
		q.nextRevision -= assigned
		return Result{}, err
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				return rollback(ErrExists)
			}
			q.items[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRevision}
			q.nextRevision++
			assigned++
			undos = append(undos, undoEntry{id: op.ID})
		case Cancel:
			prev, ok := q.items[op.ID]
			if !ok {
				return rollback(ErrNotFound)
			}
			delete(q.items, op.ID)
			undos = append(undos, undoEntry{id: op.ID, prev: prev, had: true})
		}
	}
	if len(q.items) > q.maxItems {
		return rollback(ErrCapacity)
	}
	q.now = b.Now
	q.generation++
	return Result{Generation: q.generation, Revision: q.lastRevision()}, nil
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
	sort.Slice(ready, func(i, j int) bool { return lessCanonical(ready[i], ready[j]) })
	if len(ready) > limit {
		ready = ready[:limit]
	}
	for _, it := range ready {
		delete(q.items, it.ID)
	}
	q.now = now
	return ready, nil
}

// Snapshot returns a consistent, fully detached view of the queue.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return lessCanonical(items[i], items[j]) })
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}
