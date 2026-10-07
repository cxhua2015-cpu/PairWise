package readyqueue325

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

// Queue 是并发安全的内存型就绪优先队列。
//
// 内部使用一把互斥锁保护全部状态，以 ID 为键的哈希表作为主索引；
// Pop 与 Snapshot 按需按规范顺序（Priority 降序、ReadyAt 升序、ID 升序）排序。
type Queue struct {
	mu       sync.Mutex
	items    map[string]Item
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
		items:    make(map[string]Item),
		nextRev:  1,
		maxItems: o.MaxItems,
		maxID:    o.MaxIDBytes,
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

// lastRev 返回最近一次分配的 revision，未分配过时为 0。调用方须持有锁。
func (q *Queue) lastRev() uint64 { return q.nextRev - 1 }

func (q *Queue) Apply(b Batch) (Result, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	// 先完整结构校验，再读取任何状态。
	for _, op := range b.Ops {
		if op.Kind != Enqueue && op.Kind != Cancel {
			return Result{}, ErrInvalidInput
		}
		if !validID(op.ID, q.maxID) {
			return Result{}, ErrInvalidInput
		}
		if op.ReadyAt < 0 {
			return Result{}, ErrInvalidInput
		}
	}
	if b.Now < q.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		// 空批次：generation、时间与 revision 均不变。
		return Result{Generation: q.gen, Revision: q.lastRev()}, nil
	}

	// 候选事务：在克隆的索引上顺序执行，全部成功才提交；
	// 任一步失败直接丢弃候选，时间、状态与 revision 自然回滚。
	cand := make(map[string]Item, len(q.items)+len(b.Ops))
	for k, v := range q.items {
		cand[k] = v
	}
	rev := q.nextRev
	last := q.lastRev()
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := cand[op.ID]; ok {
				return Result{}, ErrExists
			}
			cand[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: rev}
			last = rev
			rev++
		case Cancel:
			if _, ok := cand[op.ID]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.ID)
		}
	}
	// 最终容量只在末尾检查。
	if len(cand) > q.maxItems {
		return Result{}, ErrCapacity
	}

	q.items = cand
	q.now = b.Now
	q.gen++
	q.nextRev = rev
	return Result{Generation: q.gen, Revision: last}, nil
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
	// 选中与删除原子完成（同一把锁内）。
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
		Generation:   q.gen,
		NextRevision: q.nextRev,
		Now:          q.now,
		Items:        items,
	}
}
