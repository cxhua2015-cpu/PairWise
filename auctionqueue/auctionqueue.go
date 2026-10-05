package auctionqueue

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

func validateOp(op Op, maxIDBytes int) error {
	if op.Kind != Enqueue && op.Kind != Cancel {
		return ErrInvalidInput
	}
	if !validID(op.ID, maxIDBytes) {
		return ErrInvalidInput
	}
	if op.ReadyAt < 0 {
		return ErrInvalidInput
	}
	return nil
}

// less 规范顺序：Priority 降序、ReadyAt 升序、ID 升序。
func less(a, b Item) bool {
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
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

type undo struct {
	id      string
	existed bool
	prev    Item
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

	var undos []undo
	var lastRev uint64
	err := func() error {
		for _, op := range b.Ops {
			switch op.Kind {
			case Enqueue:
				if _, existed := q.items[op.ID]; existed {
					return ErrExists
				}
				undos = append(undos, undo{id: op.ID, existed: false})
				it := Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRevision}
				q.nextRevision++
				q.items[op.ID] = it
				lastRev = it.Revision
			case Cancel:
				prev, existed := q.items[op.ID]
				if !existed {
					return ErrNotFound
				}
				undos = append(undos, undo{id: op.ID, existed: true, prev: prev})
				delete(q.items, op.ID)
			}
		}
		if len(q.items) > q.maxItems {
			return ErrCapacity
		}
		return nil
	}()
	if err != nil {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.existed {
				q.items[u.id] = u.prev
			} else {
				delete(q.items, u.id)
				q.nextRevision--
			}
		}
		return Result{}, err
	}

	q.now = b.Now
	if len(b.Ops) > 0 {
		q.generation++
	}
	return Result{Generation: q.generation, Revision: lastRev}, nil
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
