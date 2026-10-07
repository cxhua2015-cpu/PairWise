package readyqueue435

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
	mu           sync.RWMutex
	maxItems     int
	maxIDBytes   int
	items        map[string]Item
	generation   uint64
	nextRevision uint64
	now          int64
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems:     o.MaxItems,
		maxIDBytes:   o.MaxIDBytes,
		items:        make(map[string]Item),
		nextRevision: 1,
	}, nil
}

func validID(id string, maxBytes int) bool {
	if id == "" || len(id) > maxBytes {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validateOpStructural checks one op without touching queue state.
func validateOpStructural(o Op, maxIDBytes int) error {
	if !validID(o.ID, maxIDBytes) {
		return ErrInvalidInput
	}
	switch o.Kind {
	case Enqueue:
		if o.ReadyAt < 0 {
			return ErrInvalidInput
		}
	case Cancel:
		// Cancel carries no payload; extra fields are rejected.
		if o.Priority != 0 || o.ReadyAt != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// validateBatchStructural performs complete structural validation of a batch.
func validateBatchStructural(b Batch, maxIDBytes int) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, o := range b.Ops {
		if err := validateOpStructural(o, maxIDBytes); err != nil {
			return err
		}
	}
	return nil
}

// less orders items canonically: Priority desc, ReadyAt asc, ID asc.
func less(a, b Item) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if a.ReadyAt != b.ReadyAt {
		return a.ReadyAt < b.ReadyAt
	}
	return a.ID < b.ID
}

func sortedItems(m map[string]Item) []Item {
	out := make([]Item, 0, len(m))
	for _, it := range m {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

// apply executes the batch against m (a working copy of the queue's items)
// and returns the last assigned revision and the next free revision.
func applyOps(m map[string]Item, nextRevision uint64, ops []Op) (lastRev, nextRev uint64, err error) {
	nextRev = nextRevision
	for _, o := range ops {
		switch o.Kind {
		case Enqueue:
			if _, ok := m[o.ID]; ok {
				return 0, 0, ErrExists
			}
			m[o.ID] = Item{ID: o.ID, Priority: o.Priority, ReadyAt: o.ReadyAt, Revision: nextRev}
			lastRev = nextRev
			nextRev++
		case Cancel:
			if _, ok := m[o.ID]; !ok {
				return 0, 0, ErrNotFound
			}
			delete(m, o.ID)
		}
	}
	return lastRev, nextRev, nil
}

func cloneItems(m map[string]Item) map[string]Item {
	out := make(map[string]Item, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (q *Queue) Apply(b Batch) (Result, error) {
	if err := validateBatchStructural(b, q.maxIDBytes); err != nil {
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
	work := cloneItems(q.items)
	lastRev, nextRev, err := applyOps(work, q.nextRevision, b.Ops)
	if err != nil {
		return Result{}, err
	}
	if len(work) > q.maxItems {
		return Result{}, ErrCapacity
	}
	q.items = work
	q.now = b.Now
	q.nextRevision = nextRev
	q.generation++
	return Result{Generation: q.generation, Revision: lastRev}, nil
}

func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	if now < 0 || limit < 0 {
		return nil, ErrInvalidInput
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	var ready []Item
	for _, it := range q.items {
		if it.ReadyAt <= now {
			ready = append(ready, it)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return less(ready[i], ready[j]) })
	if len(ready) > limit {
		ready = ready[:limit]
	}
	for _, it := range ready {
		delete(q.items, it.ID)
	}
	return ready, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return Snapshot{
		Generation:   q.generation,
		NextRevision: q.nextRevision,
		Now:          q.now,
		Items:        sortedItems(q.items),
	}
}
