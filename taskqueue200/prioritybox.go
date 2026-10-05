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

// Queue is a concurrency-safe in-memory priority task queue.
//
// Items are kept in a canonical order: Priority descending, ReadyAt
// ascending, ID ascending. A map indexes items by ID for O(1) ownership
// checks while a sorted slice maintains the canonical order.
type Queue struct {
	mu           sync.Mutex
	maxItems     int
	maxIDBytes   int
	now          int64
	generation   uint64
	nextRevision uint64 // next revision to assign, starts at 1
	byID         map[string]Item
	sorted       []Item // canonical order, same items as byID
}

// New creates a Queue. Both limits must be positive.
func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems:     o.MaxItems,
		maxIDBytes:   o.MaxIDBytes,
		nextRevision: 1,
		byID:         make(map[string]Item),
	}, nil
}

// less reports whether a sorts before b in canonical order.
func less(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

// locate returns the insertion index of it in the canonical order and
// whether an item with the same ID occupies that slot.
func locate(s []Item, it Item) (int, bool) {
	i := sort.Search(len(s), func(i int) bool { return !less(s[i], it) })
	return i, i < len(s) && s[i].ID == it.ID
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

// Apply validates the whole batch structurally, then executes the ops
// atomically in order against a candidate copy of the state. On any
// failure the time, items and revision counter are left untouched.
func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Full structural validation before reading any state.
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
	}
	if len(b.Ops) == 0 {
		return Result{Generation: q.generation, Revision: q.nextRevision - 1}, nil
	}
	if b.Now < q.now {
		return Result{}, ErrTime
	}

	// Candidate transaction: mutate copies, commit only on success.
	byID := make(map[string]Item, len(q.byID)+len(b.Ops))
	for k, v := range q.byID {
		byID[k] = v
	}
	sorted := make([]Item, len(q.sorted))
	copy(sorted, q.sorted)
	nextRevision := q.nextRevision

	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := byID[op.ID]; ok {
				return Result{}, ErrExists
			}
			it := Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRevision}
			nextRevision++
			byID[it.ID] = it
			i, _ := locate(sorted, it)
			sorted = append(sorted, Item{})
			copy(sorted[i+1:], sorted[i:])
			sorted[i] = it
		case Cancel:
			it, ok := byID[op.ID]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(byID, op.ID)
			i, found := locate(sorted, it)
			if !found {
				i = sort.Search(len(sorted), func(j int) bool { return sorted[j].ID >= op.ID })
			}
			sorted = append(sorted[:i], sorted[i+1:]...)
		}
	}
	// Capacity is only checked at the very end.
	if len(sorted) > q.maxItems {
		return Result{}, ErrCapacity
	}

	q.now = b.Now
	q.generation++
	q.nextRevision = nextRevision
	q.byID = byID
	q.sorted = sorted
	return Result{Generation: q.generation, Revision: q.nextRevision - 1}, nil
}

// Pop removes and returns up to limit ready items (ReadyAt <= now) in
// canonical order. It advances the queue clock on success.
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

	out := make([]Item, 0, limit)
	rest := q.sorted[:0]
	for _, it := range q.sorted {
		if len(out) < limit && it.ReadyAt <= now {
			delete(q.byID, it.ID)
			out = append(out, it)
			continue
		}
		rest = append(rest, it)
	}
	q.sorted = rest
	return out, nil
}

// Snapshot returns a copy of the current state; the returned slice is
// isolated from the queue's internal storage.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]Item, len(q.sorted))
	copy(items, q.sorted)
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}
