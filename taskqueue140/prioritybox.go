package taskqueue140

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

func validID(id string, maxBytes int) bool {
	if id == "" || len(id) > maxBytes {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// Queue is the transactional state engine. All methods are concurrency-safe.
type Queue struct {
	mu         sync.Mutex
	maxItems   int
	maxIDBytes int
	now        int64
	generation uint64
	nextRev    uint64
	items      map[string]Item
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems:   o.MaxItems,
		maxIDBytes: o.MaxIDBytes,
		nextRev:    1,
		items:      make(map[string]Item),
	}, nil
}

func (q *Queue) Apply(b Batch) (Result, error) {
	// Full structural validation before touching any state.
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return Result{}, ErrInvalidInput
		}
		if !validID(op.ID, q.maxIDBytes) || op.ReadyAt < 0 {
			return Result{}, ErrInvalidInput
		}
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if b.Now < q.now {
		return Result{}, ErrTime
	}

	// Apply on a copy so any failure leaves state, time and revision untouched.
	items := make(map[string]Item, len(q.items)+len(b.Ops))
	for k, v := range q.items {
		items[k] = v
	}
	nextRev := q.nextRev
	lastRev := q.nextRev - 1
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := items[op.ID]; ok {
				return Result{}, ErrExists
			}
			items[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRev}
			lastRev = nextRev
			nextRev++
		case Cancel:
			if _, ok := items[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(items, op.ID)
		}
	}
	if len(items) > q.maxItems {
		return Result{}, ErrCapacity
	}

	q.items = items
	q.now = b.Now
	q.nextRev = nextRev
	if len(b.Ops) > 0 {
		q.generation++
	}
	return Result{Generation: q.generation, Revision: lastRev}, nil
}

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
	sort.Slice(ready, func(i, j int) bool {
		a, b := ready[i], ready[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.ReadyAt != b.ReadyAt {
			return a.ReadyAt < b.ReadyAt
		}
		return a.ID < b.ID
	})
	if len(ready) > limit {
		ready = ready[:limit]
	}
	out := make([]Item, len(ready))
	copy(out, ready)
	for _, it := range ready {
		delete(q.items, it.ID)
	}
	q.now = now
	return out, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRev,
		Now:          q.now,
		Items:        items,
	}
}
