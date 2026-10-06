package readyqueue285

import (
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

// Queue is a concurrency-safe in-memory ready-priority queue.
type Queue struct {
	mu           sync.Mutex
	opts         Options
	now          int64
	generation   uint64
	nextRevision uint64
	items        map[string]Item
}

func New(opts Options) (*Queue, error) {
	if opts.MaxItems <= 0 || opts.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{opts: opts, nextRevision: 1, items: make(map[string]Item)}, nil
}

// less orders by Priority desc, ReadyAt asc, ID asc.
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
	slices.SortFunc(out, func(a, b Item) int {
		switch {
		case less(a, b):
			return -1
		case less(b, a):
			return 1
		}
		return 0
	})
	return out
}

// Apply atomically executes the batch's ops in order. On any failure the
// time, state and revision counters are left untouched.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := q.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	// Candidate transaction: mutate a private copy, commit only on success.
	items := make(map[string]Item, len(q.items)+len(b.Ops))
	for id, it := range q.items {
		items[id] = it
	}
	nextRevision := q.nextRevision
	var lastRevision uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := items[op.ID]; ok {
				return Result{}, ErrExists
			}
			items[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRevision}
			lastRevision = nextRevision
			nextRevision++
		case Cancel:
			if _, ok := items[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(items, op.ID)
		}
	}
	// Final capacity is only checked at the end.
	if len(items) > q.opts.MaxItems {
		return Result{}, ErrCapacity
	}
	q.items = items
	q.now = b.Now
	q.nextRevision = nextRevision
	if len(b.Ops) > 0 {
		q.generation++
	}
	return Result{Generation: q.generation, Revision: lastRevision}, nil
}

// Pop removes and returns up to n ready items (ReadyAt <= now) in canonical
// order: Priority desc, ReadyAt asc, ID asc.
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
	var ready []Item
	for _, it := range q.items {
		if it.ReadyAt <= now {
			ready = append(ready, it)
		}
	}
	slices.SortFunc(ready, func(a, b Item) int {
		switch {
		case less(a, b):
			return -1
		case less(b, a):
			return 1
		}
		return 0
	})
	if len(ready) > n {
		ready = ready[:n]
	}
	for _, it := range ready {
		delete(q.items, it.ID)
	}
	if ready == nil {
		ready = []Item{}
	}
	return ready, nil
}

// Snapshot returns a copy of the current state; the returned slice is
// isolated from internal state.
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
