package readyqueue245

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
// The zero Queue is not usable; construct one with New.
type Queue struct {
	mu           sync.Mutex
	maxItems     int
	maxIDBytes   int
	now          int64
	generation   uint64
	nextRevision uint64
	items        map[string]Item
}

// New validates options and returns an empty queue.
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

// Apply validates the batch, then atomically executes its ops in order.
// On any failure the time, item set and revision counter are rolled back.
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
	type undo struct {
		id      string
		prev    Item
		existed bool
	}
	var (
		undos    []undo
		lastRev  uint64
		rollback = func() {
			for i := len(undos) - 1; i >= 0; i-- {
				u := undos[i]
				if u.existed {
					q.items[u.id] = u.prev
				} else {
					delete(q.items, u.id)
				}
			}
		}
	)
	baseRev := q.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				rollback()
				q.nextRevision = baseRev
				return Result{}, ErrExists
			}
			rev := q.nextRevision
			q.nextRevision++
			q.items[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: rev}
			undos = append(undos, undo{id: op.ID})
			lastRev = rev
		case Cancel:
			prev, ok := q.items[op.ID]
			if !ok {
				rollback()
				q.nextRevision = baseRev
				return Result{}, ErrNotFound
			}
			delete(q.items, op.ID)
			undos = append(undos, undo{id: op.ID, prev: prev, existed: true})
		}
	}
	if len(q.items) > q.maxItems {
		rollback()
		q.nextRevision = baseRev
		return Result{}, ErrCapacity
	}
	q.now = b.Now
	q.generation++
	return Result{Generation: q.generation, Revision: lastRev}, nil
}

// Pop removes and returns up to n ready items (ReadyAt <= now), ordered by
// priority descending, ReadyAt ascending, ID ascending.
func (q *Queue) Pop(now int64, n int) ([]Item, error) {
	if now < 0 || n <= 0 {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if now < q.now {
		return nil, ErrTime
	}
	ready := make([]Item, 0, n)
	for _, it := range q.items {
		if it.ReadyAt <= now {
			ready = append(ready, it)
		}
	}
	sortItems(ready)
	if len(ready) > n {
		ready = ready[:n]
	}
	for _, it := range ready {
		delete(q.items, it.ID)
	}
	q.now = now
	out := make([]Item, len(ready))
	copy(out, ready)
	return out, nil
}

// Snapshot returns a consistent copy of the queue state; the returned slice
// is fully isolated from internal state.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
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

// sortItems orders items by priority descending, ReadyAt ascending, ID ascending.
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
