package readyqueue270

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

// entry is the internal mutable node tracked by both indexes.
type entry struct {
	item Item
	heap int // position in the readiness heap, -1 when not linked
}

// readyHeap orders entries by ReadyAt ascending, then ID ascending, so the
// earliest-ready task is always at the top.
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
	h[i].heap = i
	h[j].heap = j
}
func (h *readyHeap) Push(x any) {
	e := x.(*entry)
	e.heap = len(*h)
	*h = append(*h, e)
}
func (h *readyHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	old[n-1] = nil
	e.heap = -1
	*h = old[:n-1]
	return e
}

// Queue is a concurrency-safe ready-first priority queue. The zero value is
// not usable; construct it with New.
type Queue struct {
	mu      sync.Mutex
	opts    Options
	now     int64
	gen     uint64
	nextRev uint64
	byID    map[string]*entry
	ready   readyHeap
}

// New validates the options and returns an empty queue.
func New(opts Options) (*Queue, error) {
	if opts.MaxItems <= 0 || opts.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		opts:    opts,
		nextRev: 1,
		byID:    make(map[string]*entry),
	}, nil
}

// insertEntry links e into both indexes. Caller must hold q.mu.
func (q *Queue) insertEntry(e *entry) {
	q.byID[e.item.ID] = e
	heap.Push(&q.ready, e)
}

// removeEntry unlinks e from both indexes. Caller must hold q.mu.
func (q *Queue) removeEntry(e *entry) {
	delete(q.byID, e.item.ID)
	heap.Remove(&q.ready, e.heap)
}

// Apply validates the batch structurally, then executes its ops in order as
// one atomic candidate transaction. Capacity is only checked at the very end;
// any failure rolls back time, state and revision allocation.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := q.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if b.Now < q.now {
		return Result{}, ErrTime
	}

	savedRev := q.nextRev
	var undos []Kind
	var undone []*entry
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			if undos[i] == Enqueue {
				q.removeEntry(undone[i])
			} else {
				q.insertEntry(undone[i])
			}
		}
		q.nextRev = savedRev
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.byID[op.ID]; ok {
				rollback()
				return Result{}, ErrExists
			}
			e := &entry{item: Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRev}, heap: -1}
			q.nextRev++
			q.insertEntry(e)
			undos = append(undos, Enqueue)
			undone = append(undone, e)
		case Cancel:
			e, ok := q.byID[op.ID]
			if !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			q.removeEntry(e)
			undos = append(undos, Cancel)
			undone = append(undone, e)
		}
	}
	if len(q.byID) > q.opts.MaxItems {
		rollback()
		return Result{}, ErrCapacity
	}

	q.now = b.Now
	if len(b.Ops) > 0 {
		q.gen++
	}
	rev := uint64(0)
	if q.nextRev > 1 {
		rev = q.nextRev - 1
	}
	return Result{Generation: q.gen, Revision: rev}, nil
}

// Pop atomically removes up to limit ready items (ReadyAt <= now), ordered by
// Priority descending, ReadyAt ascending, ID ascending.
func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	if now < 0 || limit <= 0 {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if now < q.now {
		return nil, ErrTime
	}

	var cand []*entry
	for len(q.ready) > 0 && q.ready[0].item.ReadyAt <= now {
		cand = append(cand, heap.Pop(&q.ready).(*entry))
	}
	sort.Slice(cand, func(i, j int) bool {
		a, b := cand[i].item, cand[j].item
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.ReadyAt != b.ReadyAt {
			return a.ReadyAt < b.ReadyAt
		}
		return a.ID < b.ID
	})
	n := min(limit, len(cand))
	out := make([]Item, 0, n)
	for i, e := range cand {
		if i < n {
			out = append(out, e.item)
			delete(q.byID, e.item.ID)
		} else {
			heap.Push(&q.ready, e)
		}
	}
	q.now = now
	return out, nil
}

// Snapshot returns a consistent copy of the logical clocks and all items,
// sorted by ID ascending. The result shares no memory with the queue.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]Item, 0, len(q.byID))
	for _, e := range q.byID {
		items = append(items, e.item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return Snapshot{
		Generation:   q.gen,
		NextRevision: q.nextRev,
		Now:          q.now,
		Items:        items,
	}
}
