package readyqueue260

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

// Queue is a concurrency-safe in-memory ready-priority queue.
//
// Internal layout:
//   - items: primary hash index keyed by ID, giving O(1) existence
//     checks for Enqueue/Cancel.
//   - generation / nextRevision / now: logical clocks guarded by mu.
type Queue struct {
	mu           sync.Mutex
	opts         Options
	items        map[string]Item
	generation   uint64
	nextRevision uint64
	now          int64
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{opts: o, items: make(map[string]Item), nextRevision: 1}, nil
}

// less orders items canonically: Priority desc, ReadyAt asc, ID asc.
func less(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

// Apply validates the batch structurally, then executes its ops in order as
// one atomic transaction. Capacity is only enforced at the end. Any failure
// rolls back items, revision and the logical clock.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := q.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		return Result{Generation: q.generation, Revision: q.nextRevision - 1}, nil
	}

	type undo struct {
		id      string
		old     Item
		existed bool
	}
	journal := make([]undo, 0, len(b.Ops))
	oldRevision := q.nextRevision
	oldNow := q.now
	rollback := func() {
		for i := len(journal) - 1; i >= 0; i-- {
			u := journal[i]
			if u.existed {
				q.items[u.id] = u.old
			} else {
				delete(q.items, u.id)
			}
		}
		q.nextRevision = oldRevision
		q.now = oldNow
	}

	q.now = b.Now
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
			journal = append(journal, undo{id: op.ID})
			lastRev = rev
		case Cancel:
			old, ok := q.items[op.ID]
			if !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			delete(q.items, op.ID)
			journal = append(journal, undo{id: op.ID, old: old, existed: true})
		}
	}
	if len(q.items) > q.opts.MaxItems {
		rollback()
		return Result{}, ErrCapacity
	}
	q.generation++
	return Result{Generation: q.generation, Revision: lastRev}, nil
}

// Pop atomically removes and returns up to n items with ReadyAt <= now in
// canonical order. It advances the logical clock to now.
func (q *Queue) Pop(now int64, n int) ([]Item, error) {
	if now < 0 || n < 0 {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if now < q.now {
		return nil, ErrTime
	}
	q.now = now
	if n == 0 {
		return []Item{}, nil
	}
	ready := make([]Item, 0, len(q.items))
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
	return ready, nil
}

// Snapshot returns a consistent copy of the logical clocks and all items in
// canonical order. The returned slice is detached from internal state.
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
