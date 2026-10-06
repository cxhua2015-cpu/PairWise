package readyqueue300

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

// Queue is a concurrency-safe ready-priority queue with an explicit,
// non-negative, monotonic logical clock.
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

// Apply validates the whole batch structurally, then executes the ops
// atomically and in order under the queue lock. Any failure rolls back
// time, item state and revision allocation.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := q.validateStructural(b); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(b.Ops) == 0 {
		return Result{Generation: q.generation}, nil
	}
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	savedNow := q.now
	savedRevision := q.nextRevision
	q.now = b.Now

	type undoEntry struct {
		item    Item
		existed bool
	}
	var undos []undoEntry
	fail := func(err error) (Result, error) {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.existed {
				q.items[u.item.ID] = u.item
			} else {
				delete(q.items, u.item.ID)
			}
		}
		q.now = savedNow
		q.nextRevision = savedRevision
		return Result{}, err
	}

	var lastRevision uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				return fail(ErrExists)
			}
			it := Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRevision}
			q.nextRevision++
			q.items[op.ID] = it
			undos = append(undos, undoEntry{item: Item{ID: op.ID}})
			lastRevision = it.Revision
		case Cancel:
			it, ok := q.items[op.ID]
			if !ok {
				return fail(ErrNotFound)
			}
			delete(q.items, op.ID)
			undos = append(undos, undoEntry{item: it, existed: true})
		}
	}
	// Capacity is enforced only once, at the very end of the batch.
	if len(q.items) > q.maxItems {
		return fail(ErrCapacity)
	}
	q.generation++
	return Result{Generation: q.generation, Revision: lastRevision}, nil
}

// Pop atomically removes up to limit ready items (ReadyAt <= now), ordered
// by Priority desc, ReadyAt asc, ID asc.
func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	if now < 0 || limit <= 0 {
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
	sortItems(ready)
	if len(ready) > limit {
		ready = ready[:limit]
	}
	for _, it := range ready {
		delete(q.items, it.ID)
	}
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
	sortItems(items)
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}

// sortItems orders by Priority desc, ReadyAt asc, ID asc.
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
