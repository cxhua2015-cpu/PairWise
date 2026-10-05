package taskqueue175

import (
	"cmp"
	"errors"
	"slices"
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
// Internally it keeps a hash index keyed by task ID for O(1) existence
// checks and (re)sorts ready candidates on demand. All public methods
// serialize through a single mutex, which makes Apply batches atomic.
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
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// less orders items by Priority desc, ReadyAt asc, ID asc.
func less(a, b Item) int {
	if a.Priority != b.Priority {
		return cmp.Compare(b.Priority, a.Priority)
	}
	if a.ReadyAt != b.ReadyAt {
		return cmp.Compare(a.ReadyAt, b.ReadyAt)
	}
	return cmp.Compare(a.ID, b.ID)
}

func (q *Queue) Apply(b Batch) (Result, error) {
	// Full structural validation before touching any state.
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
		if op.Kind == Enqueue && op.ReadyAt < 0 {
			return Result{}, ErrInvalidInput
		}
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if b.Now < q.now {
		return Result{}, ErrTime
	}

	type undo struct {
		id   string
		had  bool
		prev Item
	}
	var undos []undo
	savedNext := q.nextRevision
	rollback := func(err error) (Result, error) {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.had {
				q.items[u.id] = u.prev
			} else {
				delete(q.items, u.id)
			}
		}
		q.nextRevision = savedNext
		return Result{}, err
	}

	var revision uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				return rollback(ErrExists)
			}
			it := Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRevision}
			q.items[op.ID] = it
			undos = append(undos, undo{id: op.ID})
			q.nextRevision++
			revision = it.Revision
		case Cancel:
			it, ok := q.items[op.ID]
			if !ok {
				return rollback(ErrNotFound)
			}
			delete(q.items, op.ID)
			undos = append(undos, undo{id: op.ID, had: true, prev: it})
		}
	}
	// Capacity is only checked at the very end.
	if len(q.items) > q.maxItems {
		return rollback(ErrCapacity)
	}

	q.now = b.Now
	if len(b.Ops) > 0 {
		q.generation++
	}
	return Result{Generation: q.generation, Revision: revision}, nil
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

	var ready []Item
	for _, it := range q.items {
		if it.ReadyAt <= now {
			ready = append(ready, it)
		}
	}
	slices.SortFunc(ready, less)
	if len(ready) > limit {
		ready = ready[:limit]
	}
	for _, it := range ready {
		delete(q.items, it.ID)
	}
	if ready == nil {
		ready = []Item{}
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
	slices.SortFunc(items, less)
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}
