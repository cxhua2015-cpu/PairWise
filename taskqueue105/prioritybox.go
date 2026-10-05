package taskqueue105

import (
	"container/heap"
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

// entry is the internal mutable record for a live task.
type entry struct {
	item Item
}

// canonicalLess is the pop order: Priority desc, ReadyAt asc, ID asc.
func canonicalLess(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

// readyHeap is keyed by ReadyAt asc so Pop can drain ready candidates;
// ties fall back to canonical order.
type readyHeap []Item

func (h readyHeap) Len() int { return len(h) }
func (h readyHeap) Less(i, j int) bool {
	a, b := h[i], h[j]
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return canonicalLess(a, b)
}
func (h readyHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *readyHeap) Push(x any)   { *h = append(*h, x.(Item)) }
func (h *readyHeap) Pop() (v any) {
	old := *h
	n := len(old)
	v = old[n-1]
	*h = old[:n-1]
	return v
}

type Queue struct {
	mu           sync.Mutex
	maxItems     int
	maxIDBytes   int
	now          int64
	generation   uint64
	nextRevision uint64
	items        map[string]entry
	heap         readyHeap
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems:     o.MaxItems,
		maxIDBytes:   o.MaxIDBytes,
		nextRevision: 1,
		items:        make(map[string]entry),
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

// Apply validates the whole batch structurally, then executes ops in order
// against the live state, rolling back time, state and revision on failure.
func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := q.validateBatch(b); err != nil {
		return Result{}, err
	}
	if b.Now < q.now {
		return Result{}, ErrTime
	}

	type undo struct {
		kind Kind
		item Item
	}
	var undos []undo
	revBase := q.nextRevision

	fail := func(err error) (Result, error) {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.kind == Enqueue {
				delete(q.items, u.item.ID)
			} else {
				q.items[u.item.ID] = entry{item: u.item}
			}
		}
		q.nextRevision = revBase
		q.rebuildHeapLocked()
		return Result{}, err
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				return fail(ErrExists)
			}
			it := Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRevision}
			q.nextRevision++
			q.items[op.ID] = entry{item: it}
			undos = append(undos, undo{kind: Enqueue, item: it})
		case Cancel:
			e, ok := q.items[op.ID]
			if !ok {
				return fail(ErrNotFound)
			}
			delete(q.items, op.ID)
			undos = append(undos, undo{kind: Cancel, item: e.item})
		}
	}

	if len(q.items) > q.maxItems {
		return fail(ErrCapacity)
	}

	q.now = b.Now
	if len(b.Ops) > 0 {
		q.generation++
	}
	q.rebuildHeapLocked()
	return Result{Generation: q.generation, Revision: q.nextRevision - 1}, nil
}

// rebuildHeapLocked recomputes the candidate heap from the map index.
func (q *Queue) rebuildHeapLocked() {
	h := make(readyHeap, 0, len(q.items))
	for _, e := range q.items {
		h = append(h, e.item)
	}
	heap.Init(&h)
	q.heap = h
}

// Pop atomically removes up to n ready tasks (ReadyAt <= now) in canonical
// order: Priority desc, ReadyAt asc, ID asc.
func (q *Queue) Pop(now int64, n int) ([]Item, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if now < 0 || n <= 0 {
		return nil, ErrInvalidInput
	}
	if now < q.now {
		return nil, ErrTime
	}

	// Candidate transaction: drain all ready items, sort canonically,
	// keep the best n, and push the rest back.
	var ready []Item
	for len(q.heap) > 0 && q.heap[0].ReadyAt <= now {
		ready = append(ready, heap.Pop(&q.heap).(Item))
	}
	sort.Slice(ready, func(i, j int) bool { return canonicalLess(ready[i], ready[j]) })
	out := ready
	if len(ready) > n {
		out = ready[:n]
		for _, it := range ready[n:] {
			heap.Push(&q.heap, it)
		}
	}
	for _, it := range out {
		delete(q.items, it.ID)
	}
	q.now = now
	return out, nil
}

// Snapshot returns a consistent copy of the state; Items follows canonical
// pop order and is isolated from internal state.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()

	items := make([]Item, len(q.heap))
	copy(items, q.heap)
	sort.Slice(items, func(i, j int) bool {
		return canonicalLess(items[i], items[j])
	})
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}
