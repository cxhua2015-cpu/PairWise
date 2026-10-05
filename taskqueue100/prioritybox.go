package taskqueue100

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
type Queue struct {
	mu           sync.Mutex
	maxItems     int
	maxIDBytes   int
	now          int64
	generation   uint64
	nextRevision uint64
	byID         map[string]*entry
	ready        readyHeap
}

// entry is the single owned copy of an Item. Both indexes reference it.
type entry struct {
	item Item
	idx  int // position inside readyHeap, maintained by container/heap
}

// readyHeap is a min-heap ordered by (ReadyAt asc, ID asc). It enumerates
// candidates for Pop; final ranking uses the canonical order.
type readyHeap []*entry

func (h readyHeap) Len() int { return len(h) }
func (h readyHeap) Less(i, j int) bool {
	a, b := h[i].item, h[j].item
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}
func (h readyHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].idx, h[j].idx = i, j
}
func (h *readyHeap) Push(x any) {
	e := x.(*entry)
	e.idx = len(*h)
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

// lessCanonical reports whether a ranks before b: Priority desc,
// ReadyAt asc, ID asc.
func lessCanonical(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

func validID(id string, maxBytes int) bool {
	if len(id) == 0 || len(id) > maxBytes {
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

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems:     o.MaxItems,
		maxIDBytes:   o.MaxIDBytes,
		byID:         make(map[string]*entry),
		nextRevision: 1,
	}, nil
}

func (q *Queue) add(e *entry) {
	q.byID[e.item.ID] = e
	heap.Push(&q.ready, e)
}

func (q *Queue) remove(e *entry) {
	delete(q.byID, e.item.ID)
	heap.Remove(&q.ready, e.idx)
}

// undo records one applied op so a failed batch can be rolled back.
type undo struct {
	kind Kind
	e    *entry
}

func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Structural validation of the whole batch before touching state.
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
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		return Result{Generation: q.generation}, nil
	}

	savedNext := q.nextRevision
	undos := make([]undo, 0, len(b.Ops))
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.kind == Enqueue {
				q.remove(u.e)
			} else {
				q.add(u.e)
			}
		}
		q.nextRevision = savedNext
	}

	var lastRev uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.byID[op.ID]; ok {
				rollback()
				return Result{}, ErrExists
			}
			e := &entry{item: Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRevision}}
			q.nextRevision++
			lastRev = e.item.Revision
			q.add(e)
			undos = append(undos, undo{Enqueue, e})
		case Cancel:
			e, ok := q.byID[op.ID]
			if !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			q.remove(e)
			undos = append(undos, undo{Cancel, e})
		}
	}

	// Capacity is enforced only on the final state of the batch.
	if len(q.byID) > q.maxItems {
		rollback()
		return Result{}, ErrCapacity
	}

	q.now = b.Now
	q.generation++
	return Result{Generation: q.generation, Revision: lastRev}, nil
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

	// Candidate transaction: collect ready entries, rank canonically,
	// then atomically remove the winners from both indexes.
	var cands []*entry
	for _, e := range q.ready {
		if e.item.ReadyAt <= now {
			cands = append(cands, e)
		}
	}
	sort.Slice(cands, func(i, j int) bool { return lessCanonical(cands[i].item, cands[j].item) })
	if len(cands) > k {
		cands = cands[:k]
	}
	out := make([]Item, 0, len(cands))
	for _, e := range cands {
		q.remove(e)
		out = append(out, e.item)
	}
	q.now = now
	return out, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()

	items := make([]Item, 0, len(q.byID))
	for _, e := range q.byID {
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
