package readyqueue265

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

// Queue is a concurrency-safe in-memory ready-priority queue.
type Queue struct {
	mu           sync.Mutex
	opts         Options
	now          int64
	generation   uint64
	nextRevision uint64
	items        map[string]Item
}

func New(opts Options) (*Queue, error) {
	if opts.MaxItems <= 0 || opts.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{opts: opts, nextRevision: 1, items: make(map[string]Item)}, nil
}

// less reports the canonical pop order: Priority desc, ReadyAt asc, ID asc.
func less(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

func sortItems(items []Item) {
	sort.Slice(items, func(i, j int) bool { return less(items[i], items[j]) })
}

type undoEntry struct {
	id  string
	old Item
	had bool
}

func (q *Queue) Apply(b Batch) (Result, error) {
	if err := validateBatch(b, q.opts.MaxIDBytes); err != nil {
		return Result{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		return Result{Generation: q.generation, Revision: q.nextRevision - 1}, nil
	}
	var undos []undoEntry
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.had {
				q.items[u.id] = u.old
			} else {
				delete(q.items, u.id)
			}
		}
	}
	rev := q.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				rollback()
				return Result{}, ErrExists
			}
			q.items[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: rev}
			undos = append(undos, undoEntry{id: op.ID})
			rev++
		case Cancel:
			it, ok := q.items[op.ID]
			if !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			delete(q.items, op.ID)
			undos = append(undos, undoEntry{id: op.ID, old: it, had: true})
		}
	}
	if len(q.items) > q.opts.MaxItems {
		rollback()
		return Result{}, ErrCapacity
	}
	q.nextRevision = rev
	q.now = b.Now
	q.generation++
	return Result{Generation: q.generation, Revision: rev - 1}, nil
}

func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	if now < 0 || limit <= 0 {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if now < q.now {
		return nil, ErrTime
	}
	ready := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		if it.ReadyAt <= now {
			ready = append(ready, it)
		}
	}
	sortItems(ready)
	if len(ready) > limit {
		ready = ready[:limit]
	}
	for _, it := range ready {
		delete(q.items, it.ID)
	}
	q.now = now
	return ready, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		items = append(items, it)
	}
	sortItems(items)
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        items,
	}
}
