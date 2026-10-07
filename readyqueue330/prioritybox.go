package readyqueue330

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

type entry struct {
	item  Item
	index int
}

// readyHeap orders entries by ReadyAt ascending so Pop can scan ready
// candidates by peeking at the minimum.
type readyHeap []*entry

func (h readyHeap) Len() int { return len(h) }
func (h readyHeap) Less(i, j int) bool {
	a, b := h[i].item, h[j].item
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	return a.ID < b.ID
}
func (h readyHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *readyHeap) Push(x any) {
	e := x.(*entry)
	e.index = len(*h)
	*h = append(*h, e)
}
func (h *readyHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return e
}

// Queue is a concurrency-safe in-memory ready-priority queue.
type Queue struct {
	mu           sync.Mutex
	maxItems     int
	maxIDBytes   int
	now          int64
	generation   uint64
	nextRevision uint64
	items        map[string]*entry
	byReady      readyHeap
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems:     o.MaxItems,
		maxIDBytes:   o.MaxIDBytes,
		nextRevision: 1,
		items:        make(map[string]*entry),
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

// canonical order: Priority desc, ReadyAt asc, ID asc.
func lessCanonical(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Full structural validation before touching any state.
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	if b.Now < q.now {
		return Result{}, ErrTime
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
	if len(b.Ops) == 0 {
		return Result{Generation: q.generation, Revision: q.nextRevision - 1}, nil
	}

	// Candidate transaction: clone state, mutate the clone, commit on success.
	items := make(map[string]*entry, len(q.items))
	for k, v := range q.items {
		items[k] = v
	}
	byReady := make(readyHeap, len(q.byReady))
	copy(byReady, q.byReady)
	nextRev := q.nextRevision

	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := items[op.ID]; ok {
				return Result{}, ErrExists
			}
			e := &entry{item: Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRev}}
			nextRev++
			items[op.ID] = e
			heap.Push(&byReady, e)
		case Cancel:
			e, ok := items[op.ID]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(items, op.ID)
			heap.Remove(&byReady, e.index)
		}
	}
	if len(items) > q.maxItems {
		return Result{}, ErrCapacity
	}

	// Commit.
	q.items = items
	q.byReady = byReady
	q.nextRevision = nextRev
	q.now = b.Now
	q.generation++
	return Result{Generation: q.generation, Revision: nextRev - 1}, nil
}

func (q *Queue) Pop(now int64, k int) ([]Item, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if now < 0 || k <= 0 {
		return nil, ErrInvalidInput
	}
	if now < q.now {
		return nil, ErrTime
	}

	// Collect ready candidates from the ReadyAt index, then pick the
	// best k in canonical order and remove them atomically.
	var cand []*entry
	for len(q.byReady) > 0 && q.byReady[0].item.ReadyAt <= now {
		cand = append(cand, heap.Pop(&q.byReady).(*entry))
	}
	sort.Slice(cand, func(i, j int) bool { return lessCanonical(cand[i].item, cand[j].item) })
	if len(cand) > k {
		for _, e := range cand[k:] {
			heap.Push(&q.byReady, e)
		}
		cand = cand[:k]
	}
	out := make([]Item, len(cand))
	for i, e := range cand {
		delete(q.items, e.item.ID)
		out[i] = e.item
	}
	q.now = now
	return out, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]Item, 0, len(q.items))
	for _, e := range q.items {
		items = append(items, e.item)
	}
	sort.Slice(items, func(i, j int) bool { return lessCanonical(items[i], items[j]) })
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}
