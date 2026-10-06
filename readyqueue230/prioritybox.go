package readyqueue230

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

// entry is the internal heap node; the map and the heap share the same
// *entry so a Cancel can remove the exact element the heap holds.
type entry struct {
	item Item
}

// canonicalLess orders items by the canonical pop order: Priority
// descending, ReadyAt ascending, ID ascending.
func canonicalLess(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

// entryHeap is a min-heap keyed by ReadyAt (canonical order as
// tie-break) so Pop can efficiently drain the ready candidate set.
type entryHeap []*entry

func (h entryHeap) Len() int { return len(h) }
func (h entryHeap) Less(i, j int) bool {
	a, b := h[i].item, h[j].item
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	return a.ID < b.ID
}
func (h entryHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *entryHeap) Push(x any)   { *h = append(*h, x.(*entry)) }
func (h *entryHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return e
}

// Queue is a concurrency-safe ready-priority queue guarded by a single
// mutex; every public method is a linearizable critical section.
type Queue struct {
	mu           sync.Mutex
	opts         Options
	items        map[string]*entry
	ready        entryHeap
	now          int64
	generation   uint64
	nextRevision uint64
}

// New validates options and returns an empty queue. Both limits must be
// positive, otherwise ErrInvalidOptions is returned.
func New(opts Options) (*Queue, error) {
	if opts.MaxItems <= 0 || opts.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		opts:         opts,
		items:        make(map[string]*entry),
		nextRevision: 1,
	}, nil
}

// addLocked inserts a new entry; caller must hold q.mu and ensure the ID
// is absent.
func (q *Queue) addLocked(it Item) {
	e := &entry{item: it}
	q.items[it.ID] = e
	heap.Push(&q.ready, e)
}

// removeLocked deletes the entry for id; caller must hold q.mu and ensure
// the ID is present.
func (q *Queue) removeLocked(id string) {
	e := q.items[id]
	delete(q.items, id)
	for i, x := range q.ready {
		if x == e {
			heap.Remove(&q.ready, i)
			return
		}
	}
}

// Apply structurally validates the batch, then executes its ops in order
// as one atomic candidate transaction. Capacity is enforced only after
// all ops ran. Any failure rolls back items, revisions and the clock.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := q.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		return Result{Generation: q.generation}, nil
	}
	type undo struct {
		kind Kind
		item Item
	}
	var (
		undos    []undo
		startRev = q.nextRevision
		lastRev  uint64
	)
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.kind == Enqueue {
				q.removeLocked(u.item.ID)
			} else {
				q.addLocked(u.item)
			}
		}
		q.nextRevision = startRev
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				rollback()
				return Result{}, ErrExists
			}
			it := Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRevision}
			q.nextRevision++
			lastRev = it.Revision
			q.addLocked(it)
			undos = append(undos, undo{Enqueue, it})
		case Cancel:
			e, ok := q.items[op.ID]
			if !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			undos = append(undos, undo{Cancel, e.item})
			q.removeLocked(op.ID)
		}
	}
	if len(q.items) > q.opts.MaxItems {
		rollback()
		return Result{}, ErrCapacity
	}
	q.now = b.Now
	q.generation++
	return Result{Generation: q.generation, Revision: lastRev}, nil
}

// Pop atomically removes up to limit ready items (ReadyAt <= now) in
// canonical order and advances the logical clock to now.
func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	if now < 0 || limit < 0 {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if now < q.now {
		return nil, ErrTime
	}
	// Drain the ready candidate set, select up to limit items in
	// canonical order, and re-insert the remaining candidates.
	var cand []*entry
	for q.ready.Len() > 0 && q.ready[0].item.ReadyAt <= now {
		cand = append(cand, heap.Pop(&q.ready).(*entry))
	}
	sort.Slice(cand, func(i, j int) bool { return canonicalLess(cand[i].item, cand[j].item) })
	take := limit
	if take > len(cand) {
		take = len(cand)
	}
	out := make([]Item, 0, take)
	for _, e := range cand[:take] {
		delete(q.items, e.item.ID)
		out = append(out, e.item)
	}
	for _, e := range cand[take:] {
		heap.Push(&q.ready, e)
	}
	q.now = now
	return out, nil
}

// Snapshot returns a deep, canonically ordered copy of the state; the
// returned slice shares no memory with the queue.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]Item, 0, len(q.items))
	for _, e := range q.items {
		items = append(items, e.item)
	}
	sort.Slice(items, func(i, j int) bool { return canonicalLess(items[i], items[j]) })
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}
