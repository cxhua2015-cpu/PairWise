package taskqueue185

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
	return &Queue{maxItems: o.MaxItems, maxID: o.MaxIDBytes, nextRev: 1, items: map[string]Item{}}, nil
}

func validID(id string, max int) bool {
	if len(id) == 0 || len(id) > max {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
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
		if !validID(op.ID, q.maxID) {
			return Result{}, ErrInvalidInput
		}
	}
	type undo struct {
		id      string
		item    Item
		existed bool
	}
	var undos []undo
	baseRev := q.nextRev
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.existed {
				q.items[u.id] = u.item
			} else {
				delete(q.items, u.id)
			}
		}
		q.nextRev = baseRev
	}
	var lastRev uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				rollback()
				return Result{}, ErrExists
			}
			undos = append(undos, undo{id: op.ID})
			it := Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRev}
			q.nextRev++
			lastRev = it.Revision
			q.items[op.ID] = it
		case Cancel:
			it, ok := q.items[op.ID]
			if !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			undos = append(undos, undo{id: op.ID, item: it, existed: true})
			delete(q.items, op.ID)
		}
	}
	if len(q.items) > q.maxItems {
		rollback()
		return Result{}, ErrCapacity
	}
	q.now = b.Now
	if len(b.Ops) > 0 {
		q.gen++
	}
	return Result{Generation: q.gen, Revision: lastRev}, nil
}

func (q *Queue) Pop(now int64, n int) ([]Item, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if now < 0 || n <= 0 {
		return nil, ErrInvalidInput
	}
	if now < q.now {
		return nil, ErrTime
	}
	var ready []Item
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
	out := make([]Item, len(ready))
	copy(out, ready)
	return out, nil
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
