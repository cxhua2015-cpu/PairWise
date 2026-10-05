package taskqueue200

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
	items        []Item // always kept in canonical order
	index        map[string]int
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems:     o.MaxItems,
		maxIDBytes:   o.MaxIDBytes,
		nextRevision: 1,
		index:        make(map[string]int),
	}, nil
}

func validID(id string, maxBytes int) bool {
	if id == "" || len(id) > maxBytes {
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

// less reports whether a sorts before b in canonical order:
// Priority descending, ReadyAt ascending, ID ascending.
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
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	// Full structural validation before touching any state.
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return Result{}, ErrInvalidInput
		}
		if !validID(op.ID, q.maxIDBytes) {
			return Result{}, ErrInvalidInput
		}
		if op.ReadyAt < 0 {
			return Result{}, ErrInvalidInput
		}
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if b.Now < q.now {
		return Result{}, ErrTime
	}

	// Candidate transaction: clone state, mutate the clone, commit on success.
	items := make([]Item, len(q.items))
	copy(items, q.items)
	index := make(map[string]int, len(q.index))
	for k, v := range q.index {
		index[k] = v
	}
	nextRevision := q.nextRevision
	var lastRevision uint64

	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := index[op.ID]; ok {
				return Result{}, ErrExists
			}
			it := Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRevision}
			nextRevision++
			lastRevision = it.Revision
			pos := sort.Search(len(items), func(i int) bool { return !less(items[i], it) })
			items = append(items, Item{})
			copy(items[pos+1:], items[pos:])
			items[pos] = it
			for i := pos; i < len(items); i++ {
				index[items[i].ID] = i
			}
		case Cancel:
			pos, ok := index[op.ID]
			if !ok {
				return Result{}, ErrNotFound
			}
			copy(items[pos:], items[pos+1:])
			items = items[:len(items)-1]
			delete(index, op.ID)
			for i := pos; i < len(items); i++ {
				index[items[i].ID] = i
			}
		}
	}

	// Final capacity check only at the end.
	if len(items) > q.maxItems {
		return Result{}, ErrCapacity
	}

	// Commit.
	q.items = items
	q.index = index
	q.nextRevision = nextRevision
	q.now = b.Now
	if len(b.Ops) > 0 {
		q.generation++
	}
	return Result{Generation: q.generation, Revision: lastRevision}, nil
}

func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	if now < 0 || limit <= 0 {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	out := make([]Item, 0, limit)
	rest := q.items[:0]
	for _, it := range q.items {
		if len(out) < limit && it.ReadyAt <= now {
			out = append(out, it)
		} else {
			rest = append(rest, it)
		}
	}
	q.items = rest
	q.index = make(map[string]int, len(rest))
	for i, it := range rest {
		q.index[it.ID] = i
	}
	return out, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]Item, len(q.items))
	copy(items, q.items)
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}
