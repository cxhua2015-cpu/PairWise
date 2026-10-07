package readyqueue370

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

// entry is the internal node shared by the ID index and the ready heap.
type entry struct {
	item      Item
	heapIndex int
}

// readyHeap orders candidates by ReadyAt asc, then Priority desc, then ID asc
// so the top is always a deterministic earliest-ready candidate.
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
	h[i].heapIndex = i
	h[j].heapIndex = j
}
func (h *readyHeap) Push(x any) {
	e := x.(*entry)
	e.heapIndex = len(*h)
	*h = append(*h, e)
}
func (h *readyHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	old[n-1] = nil
	e.heapIndex = -1
	*h = old[:n-1]
	return e
}

// lessCanonical is the Pop selection order: Priority desc, ReadyAt asc, ID asc.
func lessCanonical(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

type Queue struct {
	mu       sync.Mutex
	items    map[string]*entry
	ready    readyHeap
	now      int64
	gen      uint64
	nextRev  uint64
	maxItems int
	maxID    int
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		items:    make(map[string]*entry),
		nextRev:  1,
		maxItems: o.MaxItems,
		maxID:    o.MaxIDBytes,
	}, nil
}

func validID(id string, maxID int) bool {
	if len(id) == 0 || len(id) > maxID {
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

// undo records one applied op so a failed batch can be rolled back.
type undo struct {
	cancelled *entry // non-nil: a Cancel removed this entry, re-insert on rollback
	enqueued  *entry // non-nil: an Enqueue added this entry, remove on rollback
}

func (q *Queue) rollback(undos []undo, savedRev uint64) {
	for i := len(undos) - 1; i >= 0; i-- {
		u := undos[i]
		if u.enqueued != nil {
			delete(q.items, u.enqueued.item.ID)
			heap.Remove(&q.ready, u.enqueued.heapIndex)
		} else {
			q.items[u.cancelled.item.ID] = u.cancelled
			heap.Push(&q.ready, u.cancelled)
		}
	}
	q.nextRev = savedRev
}

func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Full structural validation before touching any state.
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
	if b.Now < q.now {
		return Result{}, ErrTime
	}

	savedRev := q.nextRev
	lastRev := q.nextRev - 1
	var undos []undo
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				q.rollback(undos, savedRev)
				return Result{}, ErrExists
			}
			e := &entry{item: Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRev}}
			q.nextRev++
			lastRev = e.item.Revision
			q.items[op.ID] = e
			heap.Push(&q.ready, e)
			undos = append(undos, undo{enqueued: e})
		case Cancel:
			e, ok := q.items[op.ID]
			if !ok {
				q.rollback(undos, savedRev)
				return Result{}, ErrNotFound
			}
			delete(q.items, op.ID)
			heap.Remove(&q.ready, e.heapIndex)
			undos = append(undos, undo{cancelled: e})
		}
	}
	// Final capacity check happens only at the end.
	if len(q.items) > q.maxItems {
		q.rollback(undos, savedRev)
		return Result{}, ErrCapacity
	}

	q.now = b.Now
	if len(b.Ops) > 0 {
		q.gen++
	}
	return Result{Generation: q.gen, Revision: lastRev}, nil
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

	// Gather all ready candidates from the heap, then select in canonical order.
	var cands []*entry
	for len(q.ready) > 0 && q.ready[0].item.ReadyAt <= now {
		cands = append(cands, heap.Pop(&q.ready).(*entry))
	}
	sort.Slice(cands, func(i, j int) bool { return lessCanonical(cands[i].item, cands[j].item) })

	n := limit
	if n > len(cands) {
		n = len(cands)
	}
	out := make([]Item, n)
	for i := 0; i < n; i++ {
		delete(q.items, cands[i].item.ID)
		out[i] = cands[i].item
	}
	for _, e := range cands[n:] {
		heap.Push(&q.ready, e)
	}
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
		Generation:   q.gen,
		NextRevision: q.nextRev,
		Now:          q.now,
		Items:        items,
	}
}
