package readyqueue220

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
	if id == "" || len(id) > max {
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

func less(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

func sortedItems(m map[string]Item) []Item {
	if len(m) == 0 {
		return nil
	}
	out := make([]Item, 0, len(m))
	for _, it := range m {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

func (q *Queue) Apply(b Batch) (Result, error) {
	// Structural validation of the whole batch before touching state.
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return Result{}, ErrInvalidInput
		}
		if !validID(op.ID, q.maxID) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Enqueue && op.ReadyAt < 0 {
			return Result{}, ErrInvalidInput
		}
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: q.gen}, nil
	}
	if b.Now < q.now {
		return Result{}, ErrTime
	}

	// Candidate transaction: mutate a clone, commit only on success.
	cand := make(map[string]Item, len(q.items)+len(b.Ops))
	for k, v := range q.items {
		cand[k] = v
	}
	nextRev := q.nextRev
	var lastRev uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := cand[op.ID]; ok {
				return Result{}, ErrExists
			}
			cand[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRev}
			lastRev = nextRev
			nextRev++
		case Cancel:
			if _, ok := cand[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.ID)
		}
	}
	if len(cand) > q.maxItems {
		return Result{}, ErrCapacity
	}

	q.items = cand
	q.nextRev = nextRev
	q.now = b.Now
	q.gen++
	return Result{Generation: q.gen, Revision: lastRev}, nil
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
	ready := make([]Item, 0)
	rest := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		if it.ReadyAt <= now {
			ready = append(ready, it)
		} else {
			rest = append(rest, it)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return less(ready[i], ready[j]) })
	if len(ready) > limit {
		for _, it := range ready[limit:] {
			rest = append(rest, it)
		}
		ready = ready[:limit]
	}
	q.items = make(map[string]Item, len(rest))
	for _, it := range rest {
		q.items[it.ID] = it
	}
	q.now = now
	if len(ready) == 0 {
		return nil, nil
	}
	return ready, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	return Snapshot{
		Generation:   q.gen,
		NextRevision: q.nextRev,
		Now:          q.now,
		Items:        sortedItems(q.items),
	}
}
