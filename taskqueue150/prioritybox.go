package taskqueue150

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

func (q *Queue) checkNow(now int64) error {
	if now < 0 {
		return ErrInvalidInput
	}
	if now < q.now {
		return ErrTime
	}
	return nil
}

func (q *Queue) validateOp(op Op) error {
	if op.Kind != Enqueue && op.Kind != Cancel {
		return ErrInvalidInput
	}
	if !validID(op.ID, q.maxIDBytes) {
		return ErrInvalidInput
	}
	return nil
}

type undo struct {
	kind Kind
	item Item
}

func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := q.checkNow(b.Now); err != nil {
		return Result{}, err
	}
	for _, op := range b.Ops {
		if err := q.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	undos := make([]undo, 0, len(b.Ops))
	var lastRev uint64
	var enqueued uint64
	fail := func(err error) (Result, error) {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.kind == Enqueue {
				delete(q.items, u.item.ID)
			} else {
				q.items[u.item.ID] = u.item
			}
		}
		q.nextRevision -= enqueued
		return Result{}, err
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				return fail(ErrExists)
			}
			it := Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRevision}
			q.items[op.ID] = it
			q.nextRevision++
			enqueued++
			lastRev = it.Revision
			undos = append(undos, undo{kind: Enqueue, item: it})
		case Cancel:
			it, ok := q.items[op.ID]
			if !ok {
				return fail(ErrNotFound)
			}
			delete(q.items, op.ID)
			undos = append(undos, undo{kind: Cancel, item: it})
		}
	}
	if len(q.items) > q.maxItems {
		return fail(ErrCapacity)
	}
	q.now = b.Now
	if len(b.Ops) > 0 {
		q.generation++
	}
	return Result{Generation: q.generation, Revision: lastRev}, nil
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
