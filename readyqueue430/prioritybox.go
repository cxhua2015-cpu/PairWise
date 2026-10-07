package readyqueue430

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
// Index model: items live in a hash map keyed by ID (O(1) existence
// checks for Enqueue/Cancel); canonical pop order is materialized on
// demand by sorting. The mutex serializes all public operations, which
// provides the linearization points for Apply, Pop, Snapshot, Stats,
// Clone and Preview.
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

func sortItems(items []Item) {
	sort.Slice(items, func(i, j int) bool { return less(items[i], items[j]) })
}

// Apply validates the batch structurally, then executes its ops in order
// as one atomic transaction. Capacity is only checked at the very end.
// Any failure rolls back items, revision counter and logical time.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := q.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	type undo struct {
		id   string
		item Item
		had  bool
	}
	var undos []undo
	savedRevision := q.nextRevision
	rollback := func(err error) (Result, error) {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.had {
				q.items[u.id] = u.item
			} else {
				delete(q.items, u.id)
			}
		}
		q.nextRevision = savedRevision
		return Result{}, err
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				return rollback(ErrExists)
			}
			q.items[op.ID] = Item{
				ID:       op.ID,
				Priority: op.Priority,
				ReadyAt:  op.ReadyAt,
				Revision: q.nextRevision,
			}
			q.nextRevision++
			undos = append(undos, undo{id: op.ID})
		case Cancel:
			it, ok := q.items[op.ID]
			if !ok {
				return rollback(ErrNotFound)
			}
			delete(q.items, op.ID)
			undos = append(undos, undo{id: op.ID, item: it, had: true})
		}
	}
	if len(q.items) > q.maxItems {
		return rollback(ErrCapacity)
	}
	q.now = b.Now
	if len(b.Ops) > 0 {
		q.generation++
	}
	return Result{Generation: q.generation, Revision: q.nextRevision - 1}, nil
}

// Pop atomically removes and returns up to limit ready items
// (ReadyAt <= now) in canonical order.
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
	out := make([]Item, len(ready))
	copy(out, ready)
	for _, it := range out {
		delete(q.items, it.ID)
	}
	return out, nil
}

// Snapshot returns a consistent copy of the logical clocks and all items
// in canonical order. The returned slice is fully detached.
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
