package eventqueue

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
	lastRevision uint64
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

	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	if b.Now < q.now {
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

	candidate := make(map[string]Item, len(q.items)+len(b.Ops))
	for id, it := range q.items {
		candidate[id] = it
	}
	nextRevision, lastRevision := q.nextRevision, q.lastRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := candidate[op.ID]; ok {
				return Result{}, ErrExists
			}
			candidate[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRevision}
			lastRevision = nextRevision
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
	q.nextRevision = nextRevision
	q.lastRevision = lastRevision
	if b.Now > q.now {
		q.now = b.Now
	}
	if len(b.Ops) > 0 {
		q.generation++
	}
	return Result{Generation: q.generation, Revision: lastRevision}, nil
}

func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if now < 0 || limit < 0 {
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
	if limit > len(ready) {
		limit = len(ready)
	}
	popped := make([]Item, limit)
	copy(popped, ready[:limit])
	for _, it := range popped {
		delete(q.items, it.ID)
	}
	if now > q.now {
		q.now = now
	}
	return popped, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()

	items := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return less(items[i], items[j]) })
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}
