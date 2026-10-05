package taskqueue125

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

func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if b.Now < 0 || b.Now < q.now {
		return Result{}, ErrTime
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

	items := make(map[string]Item, len(q.items)+len(b.Ops))
	for k, v := range q.items {
		items[k] = v
	}
	nextRevision := q.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := items[op.ID]; ok {
				return Result{}, ErrExists
			}
			items[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRevision}
			nextRevision++
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
	q.nextRevision = nextRevision
	q.now = b.Now
	if len(b.Ops) > 0 {
		q.generation++
	}
	return Result{Generation: q.generation, Revision: nextRevision - 1}, nil
}

func lessItem(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

func sortedItems(items map[string]Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return lessItem(out[i], out[j]) })
	return out
}

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
	sort.Slice(ready, func(i, j int) bool { return lessItem(ready[i], ready[j]) })
	if len(ready) > limit {
		ready = ready[:limit]
	}
	for _, it := range ready {
		delete(q.items, it.ID)
	}
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
