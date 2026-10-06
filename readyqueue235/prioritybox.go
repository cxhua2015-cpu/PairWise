package readyqueue235

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
// The zero value is not usable; construct with New.
type Queue struct {
	mu      sync.Mutex
	opts    Options
	now     int64
	gen     uint64
	nextRev uint64
	items   map[string]Item
}

// New validates Options and returns an empty queue.
func New(opts Options) (*Queue, error) {
	if opts.MaxItems <= 0 || opts.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{opts: opts, nextRev: 1, items: make(map[string]Item)}, nil
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

// sorted returns a copy of the given items in canonical pop order.
func sorted(items []Item) []Item {
	sort.Slice(items, func(i, j int) bool { return less(items[i], items[j]) })
	return items
}

// Apply structurally validates the batch, then atomically executes its ops
// in order against a candidate state. Capacity is checked only at the end.
// Any failure rolls back time, items and revision; generation is untouched.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := validateBatchStruct(b, q.opts.MaxIDBytes); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		q.now = b.Now
		return Result{Generation: q.gen, Revision: q.nextRev - 1}, nil
	}

	oldNow, oldNextRev := q.now, q.nextRev
	q.now = b.Now
	type undo struct {
		enqueue bool
		item    Item
	}
	undos := make([]undo, 0, len(b.Ops))
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			if undos[i].enqueue {
				delete(q.items, undos[i].item.ID)
			} else {
				q.items[undos[i].item.ID] = undos[i].item
			}
		}
		q.now, q.nextRev = oldNow, oldNextRev
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				rollback()
				return Result{}, ErrExists
			}
			it := Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRev}
			q.items[it.ID] = it
			q.nextRev++
			undos = append(undos, undo{enqueue: true, item: it})
		case Cancel:
			it, ok := q.items[op.ID]
			if !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			delete(q.items, op.ID)
			undos = append(undos, undo{item: it})
		}
	}
	if len(q.items) > q.opts.MaxItems {
		rollback()
		return Result{}, ErrCapacity
	}
	q.gen++
	return Result{Generation: q.gen, Revision: q.nextRev - 1}, nil
}

// Pop atomically removes and returns up to limit items with ReadyAt <= now,
// in canonical order. It advances the queue clock to now.
func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	if now < 0 || limit <= 0 {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if now < q.now {
		return nil, ErrTime
	}
	q.now = now
	ready := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		if it.ReadyAt <= now {
			ready = append(ready, it)
		}
	}
	sorted(ready)
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
	return Snapshot{
		Generation:   q.gen,
		NextRevision: q.nextRev,
		Now:          q.now,
		Items:        sorted(items),
	}
}
