package taskqueue110

import "errors"
import "sort"
import "sync"

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
	mu         sync.Mutex
	maxItems   int
	maxIDBytes int
	now        int64
	generation uint64
	nextRev    uint64
	items      map[string]Item
}

func New(opts Options) (*Queue, error) {
	if opts.MaxItems <= 0 || opts.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems:   opts.MaxItems,
		maxIDBytes: opts.MaxIDBytes,
		items:      make(map[string]Item),
		nextRev:    1,
	}, nil
}

func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	for _, op := range b.Ops {
		if err := q.validateOp(op); err != nil {
			return Result{}, err
		}
	}
	if len(b.Ops) == 0 {
		return q.result(), nil
	}

	// Candidate transaction: mutate a clone, commit only on full success.
	cand := make(map[string]Item, len(q.items)+len(b.Ops))
	for id, it := range q.items {
		cand[id] = it
	}
	nextRev := q.nextRev
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := cand[op.ID]; ok {
				return Result{}, ErrExists
			}
			cand[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRev}
			nextRev++
		case Cancel:
			if _, ok := cand[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.ID)
		}
	}
	if len(cand) > q.maxItems {
		return Result{}, ErrCapacity
	}

	q.items = cand
	q.nextRev = nextRev
	q.now = b.Now
	q.generation++
	return q.result(), nil
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
	out := make([]Item, len(ready))
	copy(out, ready)
	return out, nil
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
		NextRevision: q.nextRev,
		Now:          q.now,
		Items:        items,
	}
}

func (q *Queue) result() Result {
	return Result{Generation: q.generation, Revision: q.nextRev - 1}
}

func (q *Queue) validateOp(op Op) error {
	if op.Kind != Enqueue && op.Kind != Cancel {
		return ErrInvalidInput
	}
	if op.ReadyAt < 0 {
		return ErrInvalidInput
	}
	if len(op.ID) == 0 || len(op.ID) > q.maxIDBytes {
		return ErrInvalidInput
	}
	for i := 0; i < len(op.ID); i++ {
		c := op.ID[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return ErrInvalidInput
	}
	return nil
}

func sortItems(items []Item) {
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.ReadyAt != b.ReadyAt {
			return a.ReadyAt < b.ReadyAt
		}
		return a.ID < b.ID
	})
}
