package readyqueue390

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
	q.mu.Lock()
	defer q.mu.Unlock()

	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	// Full structural validation before touching any state.
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
	if len(b.Ops) == 0 {
		return Result{Generation: q.generation, Revision: q.nextRevision - 1}, nil
	}
	if b.Now < q.now {
		return Result{}, ErrTime
	}

	// Candidate transaction: stage mutations on an overlay, commit on success.
	type staged struct {
		item   Item
		delete bool
	}
	candidate := make(map[string]staged, len(b.Ops))
	nextRevision := q.nextRevision
	for _, op := range b.Ops {
		s, stagedHere := candidate[op.ID]
		_, exists := q.items[op.ID]
		switch op.Kind {
		case Enqueue:
			if (stagedHere && !s.delete) || (!stagedHere && exists) {
				return Result{}, ErrExists
			}
			candidate[op.ID] = staged{item: Item{
				ID:       op.ID,
				Priority: op.Priority,
				ReadyAt:  op.ReadyAt,
				Revision: nextRevision,
			}}
			nextRevision++
		case Cancel:
			if stagedHere {
				if s.delete {
					return Result{}, ErrNotFound
				}
				if !exists {
					// Enqueued and cancelled within the same batch: net no-op.
					delete(candidate, op.ID)
					continue
				}
				candidate[op.ID] = staged{delete: true}
				continue
			}
			if !exists {
				return Result{}, ErrNotFound
			}
			candidate[op.ID] = staged{delete: true}
		}
	}
	size := len(q.items)
	for id, s := range candidate {
		_, exists := q.items[id]
		if s.delete {
			size--
		} else if !exists {
			size++
		}
	}
	// Final capacity is only checked at the end.
	if size > q.maxItems {
		return Result{}, ErrCapacity
	}

	// Commit.
	for id, s := range candidate {
		if s.delete {
			delete(q.items, id)
		} else {
			q.items[id] = s.item
		}
	}
	q.now = b.Now
	q.generation++
	q.nextRevision = nextRevision
	return Result{Generation: q.generation, Revision: nextRevision - 1}, nil
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
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}
