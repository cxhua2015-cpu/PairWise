package readyqueue375

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
type Queue struct {
	mu       sync.Mutex
	maxItems int
	maxID    int
	now      int64
	gen      uint64
	nextRev  uint64
	items    map[string]Item
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems: o.MaxItems,
		maxID:    o.MaxIDBytes,
		nextRev:  1,
		items:    make(map[string]Item),
	}, nil
}

func validID(id string, max int) bool {
	if len(id) == 0 || len(id) > max {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func less(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	// Structural validation of the whole batch before touching state.
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return Result{}, ErrInvalidInput
		}
		if !validID(op.ID, q.maxID) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Enqueue {
			if op.ReadyAt < 0 {
				return Result{}, ErrInvalidInput
			}
		} else if op.Priority != 0 || op.ReadyAt != 0 {
			return Result{}, ErrInvalidInput
		}
	}
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	if len(b.Ops) == 0 {
		return Result{Generation: q.gen, Revision: q.nextRev - 1}, nil
	}
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	// Candidate transaction: mutate a clone, commit on success.
	cand := make(map[string]Item, len(q.items)+len(b.Ops))
	for k, v := range q.items {
		cand[k] = v
	}
	rev := q.nextRev
	for _, op := range b.Ops {
		if op.Kind == Enqueue {
			if _, ok := cand[op.ID]; ok {
				return Result{}, ErrExists
			}
			cand[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: rev}
			rev++
		} else {
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
	q.nextRev = rev
	q.gen++
	return Result{Generation: q.gen, Revision: rev - 1}, nil
}

func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if now < 0 || limit <= 0 {
		return nil, ErrInvalidInput
	}
	if now < q.now {
		return nil, ErrTime
	}
	ready := make([]Item, 0, len(q.items))
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

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return less(items[i], items[j]) })
	return Snapshot{Generation: q.gen, NextRevision: q.nextRev, Now: q.now, Items: items}
}
