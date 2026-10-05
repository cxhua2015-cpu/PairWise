package taskqueue120

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

// less reports whether a sorts before b in canonical pop order:
// Priority descending, ReadyAt ascending, ID ascending.
func less(a, b *Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

type itemHeap []*Item

func (h itemHeap) Len() int           { return len(h) }
func (h itemHeap) Less(i, j int) bool { return less(h[i], h[j]) }
func (h itemHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *itemHeap) Push(x any)        { *h = append(*h, x.(*Item)) }
func (h *itemHeap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return it
}

type Queue struct {
	mu           sync.Mutex
	items        map[string]*Item
	ready        itemHeap
	generation   uint64
	nextRevision uint64
	now          int64
	maxItems     int
	maxIDBytes   int
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		items:        make(map[string]*Item),
		nextRevision: 1,
		maxItems:     o.MaxItems,
		maxIDBytes:   o.MaxIDBytes,
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

func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: q.generation}, nil
	}
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
		if op.Kind == Enqueue && op.ReadyAt < 0 {
			return Result{}, ErrInvalidInput
		}
	}
	if b.Now < q.now {
		return Result{}, ErrTime
	}

	// Candidate transaction: mutate clones, commit only on success.
	items := make(map[string]*Item, len(q.items))
	for k, v := range q.items {
		items[k] = v
	}
	ready := make(itemHeap, len(q.ready))
	copy(ready, q.ready)

	nextRev := q.nextRevision
	var lastRev uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := items[op.ID]; ok {
				return Result{}, ErrExists
			}
			it := &Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRev}
			nextRev++
			lastRev = it.Revision
			items[it.ID] = it
			heap.Push(&ready, it)
		case Cancel:
			it, ok := items[op.ID]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(items, op.ID)
			for i, x := range ready {
				if x == it {
					heap.Remove(&ready, i)
					break
				}
			}
		}
	}
	if len(items) > q.maxItems {
		return Result{}, ErrCapacity
	}

	q.items = items
	q.ready = ready
	q.nextRevision = nextRev
	q.now = b.Now
	q.generation++
	return Result{Generation: q.generation, Revision: lastRev}, nil
}

func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if now < 0 || limit < 1 {
		return nil, ErrInvalidInput
	}
	if now < q.now {
		return nil, ErrTime
	}
	q.now = now

	out := make([]Item, 0, limit)
	for len(out) < limit && len(q.ready) > 0 {
		top := q.ready[0]
		if top.ReadyAt > now {
			break
		}
		heap.Pop(&q.ready)
		delete(q.items, top.ID)
		out = append(out, *top)
	}
	return out, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()

	items := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		items = append(items, *it)
	}
	sort.Slice(items, func(i, j int) bool { return less(&items[i], &items[j]) })
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}
