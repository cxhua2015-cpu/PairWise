package readyqueue205

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

type Queue struct {
	mu        sync.Mutex
	maxItems  int
	maxIDByte int
	now       int64
	gen       uint64
	nextRev   uint64
	items     map[string]Item
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		maxItems:  o.MaxItems,
		maxIDByte: o.MaxIDBytes,
		nextRev:   1,
		items:     make(map[string]Item),
	}, nil
}

func validID(id string, max int) bool {
	if len(id) == 0 || len(id) > max {
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

// less 规范顺序：Priority 降序、ReadyAt 升序、ID 升序。
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
	items := make([]Item, 0, len(m))
	for _, it := range m {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return less(items[i], items[j]) })
	return items
}

func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// 先完整结构校验，再读取状态。
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return Result{}, ErrInvalidInput
		}
		if !validID(op.ID, q.maxIDByte) {
			return Result{}, ErrInvalidInput
		}
	}
	if len(b.Ops) == 0 {
		return Result{Generation: q.gen, Revision: q.nextRev - 1}, nil
	}
	if b.Now < q.now {
		return Result{}, ErrTime
	}

	// 候选事务：在克隆状态上顺序执行，失败即丢弃，实现回滚。
	items := make(map[string]Item, len(q.items)+len(b.Ops))
	for k, v := range q.items {
		items[k] = v
	}
	nextRev := q.nextRev
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := items[op.ID]; ok {
				return Result{}, ErrExists
			}
			items[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: nextRev}
			nextRev++
		case Cancel:
			if _, ok := items[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(items, op.ID)
		}
	}
	// 最终容量只在末尾检查。
	if len(items) > q.maxItems {
		return Result{}, ErrCapacity
	}

	q.items = items
	q.nextRev = nextRev
	q.now = b.Now
	q.gen++
	return Result{Generation: q.gen, Revision: q.nextRev - 1}, nil
}

func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if now < 0 || limit < 0 {
		return nil, ErrInvalidInput
	}
	if now < q.now {
		return nil, ErrTime
	}
	q.now = now
	if limit == 0 {
		return []Item{}, nil
	}
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
	if ready == nil {
		ready = []Item{}
	}
	return ready, nil
}

func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	return Snapshot{
		Generation:   q.gen,
		NextRevision: q.nextRev,
		Now:          q.now,
		Items:        sortedItems(q.items),
	}
}
