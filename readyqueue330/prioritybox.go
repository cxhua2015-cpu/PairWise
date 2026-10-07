package readyqueue330

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
	// Structural validation of the whole batch before touching state.
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
		if op.ReadyAt < 0 {
			return Result{}, ErrInvalidInput
		}
	}
	if len(b.Ops) == 0 {
		q.mu.Lock()
		g := q.generation
		q.mu.Unlock()
		return Result{Generation: g}, nil
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if b.Now < q.now {
		return Result{}, ErrTime
	}

	// Candidate transaction: apply ops on a copy so failure rolls back
	// time, state and revision automatically.
	items := make(map[string]Item, len(q.items)+len(b.Ops))
	for k, v := range q.items {
		items[k] = v
	}
	nextRevision := q.nextRevision
	var lastRevision uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := items[op.ID]; ok {
				return Result{}, ErrExists
			}
			items[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRevision}
			lastRevision = nextRevision
			nextRevision++
		case Cancel:
			if _, ok := items[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(items, op.ID)
		}
	}
	// Final capacity check happens only at the end.
	if len(items) > q.maxItems {
		return Result{}, ErrCapacity
	}

	q.items = items
	q.now = b.Now
	q.generation++
	q.nextRevision = nextRevision
	return Result{Generation: q.generation, Revision: lastRevision}, nil
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
	q.now = now

	sorted := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		sorted = append(sorted, it)
	}
	sort.Slice(sorted, func(i, j int) bool { return less(sorted[i], sorted[j]) })

	out := make([]Item, 0, limit)
	for _, it := range sorted {
		if len(out) >= limit {
			break
		}
		if it.ReadyAt > now {
			continue
		}
		out = append(out, it)
		delete(q.items, it.ID)
	}
	return out, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        make([]Item, 0, len(q.items)),
	}
	for _, it := range q.items {
		s.Items = append(s.Items, it)
	}
	sort.Slice(s.Items, func(i, j int) bool { return less(s.Items[i], s.Items[j]) })
	return s
}
