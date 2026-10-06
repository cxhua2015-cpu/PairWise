package readyqueue270

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

// Queue is a concurrency-safe ready-priority queue. The zero value is not
// usable; construct it with New.
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

// Apply validates the batch structurally, then executes its ops in order as
// one atomic candidate transaction. On any failure the time, items and
// revision counter are left untouched.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := validateBatchStruct(b, q.maxIDBytes); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if b.Now < q.now {
		return Result{}, ErrTime
	}
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
	if len(cand) > q.maxItems {
		return Result{}, ErrCapacity
	}
	q.items = cand
	q.now = b.Now
	q.nextRevision = nextRev
	if len(b.Ops) > 0 {
		q.generation++
	}
	return Result{Generation: q.generation, Revision: lastRev}, nil
}

// Pop atomically removes and returns up to limit items with ReadyAt <= now,
// ordered by Priority desc, ReadyAt asc, ID asc.
func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	if now < 0 || limit <= 0 {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	var ready []Item
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

// Snapshot returns an isolated copy of the current state.
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
