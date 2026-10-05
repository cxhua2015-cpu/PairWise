package taskqueue190

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

func validateOp(o Op, maxIDBytes int) error {
	if o.Kind != Enqueue && o.Kind != Cancel {
		return ErrInvalidInput
	}
	if !validID(o.ID, maxIDBytes) {
		return ErrInvalidInput
	}
	if o.ReadyAt < 0 {
		return ErrInvalidInput
	}
	return nil
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
	out := make([]Item, 0, len(m))
	for _, it := range m {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
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
	for _, o := range b.Ops {
		if err := validateOp(o, q.maxIDBytes); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		q.now = b.Now
		return Result{Generation: q.generation}, nil
	}

	// Candidate transaction: mutate a clone, commit only on success.
	cand := make(map[string]Item, len(q.items)+len(b.Ops))
	for k, v := range q.items {
		cand[k] = v
	}
	nextRev := q.nextRevision
	var lastRev uint64
	for _, o := range b.Ops {
		switch o.Kind {
		case Enqueue:
			if _, ok := cand[o.ID]; ok {
				return Result{}, ErrExists
			}
			cand[o.ID] = Item{ID: o.ID, Priority: o.Priority, ReadyAt: o.ReadyAt, Revision: nextRev}
			lastRev = nextRev
			nextRev++
		case Cancel:
			if _, ok := cand[o.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, o.ID)
		}
	}
	if len(cand) > q.maxItems {
		return Result{}, ErrCapacity
	}

	q.items = cand
	q.nextRevision = nextRev
	q.now = b.Now
	q.generation++
	return Result{Generation: q.generation, Revision: lastRev}, nil
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
	q.now = now

	all := sortedItems(q.items)
	out := make([]Item, 0, limit)
	for _, it := range all {
		if it.ReadyAt > now || len(out) == limit {
			continue
		}
		out = append(out, it)
	}
	for _, it := range out {
		delete(q.items, it.ID)
	}
	return out, nil
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
