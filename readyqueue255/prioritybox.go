package readyqueue255

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

// Queue is a concurrency-safe ready-priority queue with an explicit
// non-negative monotonic clock.
type Queue struct {
	mu           sync.Mutex
	opts         Options
	items        map[string]Item
	generation   uint64
	nextRevision uint64
	now          int64
}

func New(opts Options) (*Queue, error) {
	if opts.MaxItems <= 0 || opts.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{opts: opts, items: make(map[string]Item), nextRevision: 1}, nil
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

// Apply validates the whole batch structurally, then executes ops in order
// and checks final capacity last. Any failure rolls back time, state and
// revisions.
func (q *Queue) Apply(b Batch) (Result, error) {
	if err := validateBatchStruct(b, q.opts.MaxIDBytes); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	type undo struct {
		op   Kind
		item Item
	}
	var (
		hist         []undo
		lastRevision uint64
		enqueued     uint64
	)
	rollback := func() {
		for i := len(hist) - 1; i >= 0; i-- {
			h := hist[i]
			if h.op == Enqueue {
				delete(q.items, h.item.ID)
			} else {
				q.items[h.item.ID] = h.item
			}
		}
		q.nextRevision -= enqueued
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				rollback()
				return Result{}, ErrExists
			}
			it := Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRevision}
			q.items[op.ID] = it
			hist = append(hist, undo{Enqueue, it})
			lastRevision = it.Revision
			enqueued++
			q.nextRevision++
		case Cancel:
			it, ok := q.items[op.ID]
			if !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			delete(q.items, op.ID)
			hist = append(hist, undo{Cancel, it})
		}
	}
	if len(q.items) > q.opts.MaxItems {
		rollback()
		return Result{}, ErrCapacity
	}
	if len(b.Ops) > 0 {
		q.generation++
	}
	q.now = b.Now
	return Result{Generation: q.generation, Revision: lastRevision}, nil
}

// Pop atomically removes up to n ready items (ReadyAt <= now), ordered by
// priority desc, ReadyAt asc, ID asc.
func (q *Queue) Pop(now int64, n int) ([]Item, error) {
	if now < 0 || n <= 0 {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if now < q.now {
		return nil, ErrTime
	}
	q.now = now
	out := make([]Item, 0, n)
	for len(out) < n {
		var best *Item
		for _, it := range q.items {
			if it.ReadyAt > now {
				continue
			}
			if best == nil || less(it, *best) {
				c := it
				best = &c
			}
		}
		if best == nil {
			break
		}
		delete(q.items, best.ID)
		out = append(out, *best)
	}
	return out, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return less(items[i], items[j]) })
	return Snapshot{Generation: q.generation, NextRevision: q.nextRevision, Now: q.now, Items: items}
}
