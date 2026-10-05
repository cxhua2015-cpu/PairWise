package taskqueue160

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

// Queue is a concurrency-safe in-memory priority task queue.
//
// Internally it keeps a primary index map[id]Item plus an undo log per
// in-flight batch ("candidate transaction"): mutations are staged against
// the live index and rolled back in reverse order on any failure, so a
// failed Apply leaves time, state and revisions untouched.
type Queue struct {
	mu         sync.Mutex
	maxItems   int
	maxIDBytes int
	now        int64
	generation uint64
	nextRev    uint64 // next revision to assign; starts at 1
	items      map[string]Item
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems:   o.MaxItems,
		maxIDBytes: o.MaxIDBytes,
		nextRev:    1,
		items:      make(map[string]Item),
	}, nil
}

func validID(id string, maxBytes int) bool {
	if len(id) == 0 || len(id) > maxBytes {
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

// validateBatch performs full structural validation before any state read.
func (q *Queue) validateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return ErrInvalidInput
		}
		if !validID(op.ID, q.maxIDBytes) {
			return ErrInvalidInput
		}
		if op.ReadyAt < 0 {
			return ErrInvalidInput
		}
	}
	return nil
}

type undo struct {
	added string // ID enqueued (removed on rollback), or ""
	item  Item   // item cancelled (restored on rollback)
}

func (q *Queue) Apply(b Batch) (Result, error) {
	if err := q.validateBatch(b); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		q.now = b.Now
		return Result{Generation: q.generation, Revision: q.nextRev - 1}, nil
	}

	log := make([]undo, 0, len(b.Ops))
	rollback := func(err error) (Result, error) {
		for i := len(log) - 1; i >= 0; i-- {
			u := log[i]
			if u.added != "" {
				delete(q.items, u.added)
			} else {
				q.items[u.item.ID] = u.item
			}
		}
		return Result{}, err
	}

	savedRev := q.nextRev
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				q.nextRev = savedRev
				return rollback(ErrExists)
			}
			q.items[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRev}
			q.nextRev++
			log = append(log, undo{added: op.ID})
		case Cancel:
			it, ok := q.items[op.ID]
			if !ok {
				q.nextRev = savedRev
				return rollback(ErrNotFound)
			}
			delete(q.items, op.ID)
			log = append(log, undo{item: it})
		}
	}
	// Final capacity is only checked at the end.
	if len(q.items) > q.maxItems {
		q.nextRev = savedRev
		return rollback(ErrCapacity)
	}
	q.now = b.Now
	q.generation++
	return Result{Generation: q.generation, Revision: q.nextRev - 1}, nil
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

func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	if now < 0 || limit <= 0 {
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
		NextRevision: q.nextRev,
		Now:          q.now,
		Items:        items,
	}
}
