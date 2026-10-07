package readyqueue310

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
	mu           sync.Mutex
	items        map[string]Item
	now          int64
	generation   uint64
	nextRevision uint64
	maxItems     int
	maxIDBytes   int
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{items: make(map[string]Item), nextRevision: 1, maxItems: o.MaxItems, maxIDBytes: o.MaxIDBytes}, nil
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

func sortedItems(m map[string]Item) []Item {
	items := make([]Item, 0, len(m))
	for _, it := range m {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return less(items[i], items[j]) })
	return items
}

func (q *Queue) Apply(b Batch) (Result, error) {
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return Result{}, ErrInvalidInput
		}
		if !validID(op.ID, q.maxIDBytes) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Enqueue && op.ReadyAt < 0 {
			return Result{}, ErrInvalidInput
		}
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if b.Now < q.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		return Result{Generation: q.generation, Revision: q.nextRevision - 1}, nil
	}

	candidate := make(map[string]Item, len(q.items)+len(b.Ops))
	for id, it := range q.items {
		candidate[id] = it
	}
	nextRevision := q.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := candidate[op.ID]; ok {
				return Result{}, ErrExists
			}
			candidate[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRevision}
			nextRevision++
		case Cancel:
			if _, ok := candidate[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.ID)
		}
	}
	if len(candidate) > q.maxItems {
		return Result{}, ErrCapacity
	}

	q.items = candidate
	q.now = b.Now
	q.generation++
	q.nextRevision = nextRevision
	return Result{Generation: q.generation, Revision: q.nextRevision - 1}, nil
}

func (q *Queue) Pop(now int64, n int) ([]Item, error) {
	if now < 0 || n <= 0 {
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
	sort.Slice(ready, func(i, j int) bool { return less(ready[i], ready[j]) })
	if len(ready) > n {
		ready = ready[:n]
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
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        sortedItems(q.items),
	}
}
