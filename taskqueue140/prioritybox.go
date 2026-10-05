package taskqueue140

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

// Queue is the transactional state engine. All public methods are
// concurrency-safe; a single mutex serializes Apply/Pop/Snapshot.
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

// validateOp performs structural validation only; it must not read state.
func (q *Queue) validateOp(op Op) error {
	if op.Kind != Enqueue && op.Kind != Cancel {
		return ErrInvalidInput
	}
	if !validID(op.ID, q.maxIDBytes) {
		return ErrInvalidInput
	}
	if op.Kind == Enqueue && op.ReadyAt < 0 {
		return ErrInvalidInput
	}
	return nil
}

func (q *Queue) Apply(b Batch) (Result, error) {
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	// Full structural validation of the batch before touching state.
	for _, op := range b.Ops {
		if err := q.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if b.Now < q.now {
		return Result{}, ErrTime
	}

	// Candidate transaction: stage mutations on a clone so any failure
	// leaves time, state and revision untouched.
	type change struct {
		id      string
		item    Item
		enqueue bool
	}
	changes := make([]change, 0, len(b.Ops))
	pending := make(map[string]bool, len(b.Ops)) // IDs enqueued in this batch
	removed := make(map[string]bool, len(b.Ops)) // IDs cancelled in this batch
	nextRev := q.nextRevision
	var lastRev uint64

	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			_, exists := q.items[op.ID]
			if removed[op.ID] {
				exists = false
			}
			if exists || pending[op.ID] {
				return Result{}, ErrExists
			}
			it := Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRev}
			lastRev = nextRev
			nextRev++
			pending[op.ID] = true
			delete(removed, op.ID)
			changes = append(changes, change{id: op.ID, item: it, enqueue: true})
		case Cancel:
			_, exists := q.items[op.ID]
			if pending[op.ID] {
				exists = true
			}
			if !exists || removed[op.ID] {
				return Result{}, ErrNotFound
			}
			removed[op.ID] = true
			delete(pending, op.ID)
			changes = append(changes, change{id: op.ID})
		}
	}

	// Final capacity check happens only at the end.
	finalSize := len(q.items)
	for _, c := range changes {
		if c.enqueue {
			finalSize++
		} else {
			finalSize--
		}
	}
	if finalSize > q.maxItems {
		return Result{}, ErrCapacity
	}

	// Commit.
	for _, c := range changes {
		if c.enqueue {
			q.items[c.id] = c.item
		} else {
			delete(q.items, c.id)
		}
	}
	q.now = b.Now
	q.nextRevision = nextRev
	if len(b.Ops) > 0 {
		q.generation++
	}
	return Result{Generation: q.generation, Revision: lastRev}, nil
}

// less orders items by Priority desc, ReadyAt asc, ID asc.
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
	return append([]Item(nil), ready...), nil
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
