package taskqueue180

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
	items        map[string]*Item
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{maxItems: o.MaxItems, maxIDBytes: o.MaxIDBytes, nextRevision: 1, items: make(map[string]*Item)}, nil
}

func (q *Queue) Apply(b Batch) (Result, error) {
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	for _, op := range b.Ops {
		if err := validateOp(op, q.maxIDBytes); err != nil {
			return Result{}, err
		}
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if b.Now < q.now {
		return Result{}, ErrTime
	}

	savedNow, savedGeneration, savedNextRevision := q.now, q.generation, q.nextRevision
	type undo struct {
		id      string
		removed *Item
	}
	var undos []undo
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.removed != nil {
				q.items[u.id] = u.removed
			} else {
				delete(q.items, u.id)
			}
		}
		q.now, q.generation, q.nextRevision = savedNow, savedGeneration, savedNextRevision
	}

	var lastRevision uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				rollback()
				return Result{}, ErrExists
			}
			it := &Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRevision}
			q.items[op.ID] = it
			undos = append(undos, undo{id: op.ID})
			lastRevision = q.nextRevision
			q.nextRevision++
		case Cancel:
			it, ok := q.items[op.ID]
			if !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			delete(q.items, op.ID)
			undos = append(undos, undo{id: op.ID, removed: it})
		}
	}

	if len(q.items) > q.maxItems {
		rollback()
		return Result{}, ErrCapacity
	}

	q.now = b.Now
	if len(b.Ops) > 0 {
		q.generation++
	}
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
	ready := make([]*Item, 0, len(q.items))
	for _, it := range q.items {
		if it.ReadyAt <= now {
			ready = append(ready, it)
		}
	}
	sortItems(ready)
	if len(ready) > limit {
		ready = ready[:limit]
	}
	out := make([]Item, len(ready))
	for i, it := range ready {
		out[i] = *it
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
		items = append(items, *it)
	}
	sort.Slice(items, func(i, j int) bool { return lessItem(items[i], items[j]) })
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}

func validateOp(op Op, maxIDBytes int) error {
	if op.Kind != Enqueue && op.Kind != Cancel {
		return ErrInvalidInput
	}
	if op.ReadyAt < 0 {
		return ErrInvalidInput
	}
	return validateID(op.ID, maxIDBytes)
}

func validateID(id string, maxIDBytes int) error {
	if len(id) == 0 || len(id) > maxIDBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
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

func sortItems(items []*Item) {
	sort.Slice(items, func(i, j int) bool { return lessItem(*items[i], *items[j]) })
}
