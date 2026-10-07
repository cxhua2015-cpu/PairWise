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
// State model: items are stored in a map keyed by ID; the canonical
// ordering (Priority desc, ReadyAt asc, ID asc) is computed on read.
// The logical clock now is explicit, non-negative and monotonic.
type Queue struct {
	mu           sync.Mutex
	maxItems     int
	maxIDBytes   int
	now          int64
	generation   uint64
	nextRevision uint64
	items        map[string]Item
}

// New creates an empty queue. Capacity and length limits must be positive.
func New(opts Options) (*Queue, error) {
	if opts.MaxItems <= 0 || opts.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems:     opts.MaxItems,
		maxIDBytes:   opts.MaxIDBytes,
		nextRevision: 1,
		items:        make(map[string]Item),
	}, nil
}

// Apply atomically executes the batch's ops in order. On any failure the
// logical clock, item set and revision counter are rolled back.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := q.validateBatch(b); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if b.Now < q.now {
		return Result{}, ErrTime
	}

	savedNow := q.now
	savedNext := q.nextRevision
	savedItems := make(map[string]Item, len(q.items))
	for id, it := range q.items {
		savedItems[id] = it
	}
	rollback := func(err error) (Result, error) {
		q.now = savedNow
		q.nextRevision = savedNext
		q.items = savedItems
		return Result{}, err
	}

	q.now = b.Now
	var lastRev uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				return rollback(ErrExists)
			}
			q.items[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRevision}
			lastRev = q.nextRevision
			q.nextRevision++
		case Cancel:
			if _, ok := q.items[op.ID]; !ok {
				return rollback(ErrNotFound)
			}
			delete(q.items, op.ID)
		}
	}
	// Final capacity is checked only at the end.
	if len(q.items) > q.maxItems {
		return rollback(ErrCapacity)
	}
	if len(b.Ops) > 0 {
		q.generation++
	}
	if lastRev == 0 {
		lastRev = q.nextRevision - 1
	}
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
	q.now = now
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

// Snapshot returns a copy of the state in canonical order, isolated from
// the queue's internal storage.
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

// sortItems orders items by Priority desc, ReadyAt asc, ID asc.
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
