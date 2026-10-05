package taskqueue135

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
	items        map[string]Item
	generation   uint64
	nextRevision uint64
	now          int64
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

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems:     o.MaxItems,
		maxIDBytes:   o.MaxIDBytes,
		items:        make(map[string]Item),
		nextRevision: 1,
	}, nil
}

func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Full structural validation before touching state.
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
	if b.Now < q.now {
		return Result{}, ErrTime
	}

	type undo struct {
		inserted string
		removed  *Item
	}
	undos := make([]undo, 0, len(b.Ops))
	savedRevision := q.nextRevision
	rollback := func() {
		q.nextRevision = savedRevision
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.inserted != "" {
				delete(q.items, u.inserted)
			}
			if u.removed != nil {
				q.items[u.removed.ID] = *u.removed
			}
		}
	}

	var lastRev uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				rollback()
				return Result{}, ErrExists
			}
			rev := q.nextRevision
			q.nextRevision++
			q.items[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: rev}
			undos = append(undos, undo{inserted: op.ID})
			lastRev = rev
		case Cancel:
			it, ok := q.items[op.ID]
			if !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			delete(q.items, op.ID)
			removed := it
			undos = append(undos, undo{removed: &removed})
		}
	}
	// Capacity is only checked at the very end.
	if len(q.items) > q.maxItems {
		rollback()
		return Result{}, ErrCapacity
	}

	q.now = b.Now
	if len(b.Ops) > 0 {
		q.generation++
	}
	return Result{Generation: q.generation, Revision: lastRev}, nil
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
	cands := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		if it.ReadyAt <= now {
			cands = append(cands, it)
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.ReadyAt != b.ReadyAt {
			return a.ReadyAt < b.ReadyAt
		}
		return a.ID < b.ID
	})
	if len(cands) > limit {
		cands = cands[:limit]
	}
	out := make([]Item, len(cands))
	copy(out, cands)
	for _, it := range out {
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
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}
