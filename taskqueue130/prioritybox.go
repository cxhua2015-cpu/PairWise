package taskqueue130

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

// Queue 是并发安全的内存型任务优先队列。
//
// 索引：items 以 ID 为键的哈希索引，Enqueue/Cancel 为 O(1) 均摊；
// Pop/Snapshot 在候选集上按规范顺序 (Priority 降序, ReadyAt 升序, ID 升序) 排序。
type Queue struct {
	mu      sync.Mutex
	items   map[string]Item
	now     int64
	gen     uint64
	nextRev uint64
	max     int
	maxID   int
}

func New(o Options) (*Queue, error) {
	if o.MaxItems <= 0 || o.MaxIDBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Queue{
		items:   make(map[string]Item),
		nextRev: 1,
		max:     o.MaxItems,
		maxID:   o.MaxIDBytes,
	}, nil
}

func validID(id string, maxID int) bool {
	if id == "" || len(id) > maxID {
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

func validOp(op Op, maxID int) bool {
	if op.Kind != Enqueue && op.Kind != Cancel {
		return false
	}
	if op.ReadyAt < 0 {
		return false
	}
	return validID(op.ID, maxID)
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

// Apply 原子地按顺序执行批次中的 Enqueue/Cancel。
// 先做完整结构校验，再检查时间单调性，最后执行并在末尾检查容量；
// 任何失败都会回滚时间、状态和 revision。
func (q *Queue) Apply(b Batch) (Result, error) {
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	for _, op := range b.Ops {
		if !validOp(op, q.maxID) {
			return Result{}, ErrInvalidInput
		}
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if b.Now < q.now {
		return Result{}, ErrTime
	}

	type undo struct {
		id      string
		existed bool
		prev    Item
	}
	undos := make([]undo, 0, len(b.Ops))
	savedRev := q.nextRev
	savedNow := q.now
	rollback := func() {
		for i := len(undos) - 1; i >= 0; i-- {
			u := undos[i]
			if u.existed {
				q.items[u.id] = u.prev
			} else {
				delete(q.items, u.id)
			}
		}
		q.nextRev = savedRev
		q.now = savedNow
	}

	q.now = b.Now
	for _, op := range b.Ops {
		switch op.Kind {
		case Enqueue:
			if _, ok := q.items[op.ID]; ok {
				rollback()
				return Result{}, ErrExists
			}
			undos = append(undos, undo{id: op.ID})
			q.items[op.ID] = Item{ID: op.ID, Priority: op.Priority, ReadyAt: op.ReadyAt, Revision: q.nextRev}
			q.nextRev++
		case Cancel:
			prev, ok := q.items[op.ID]
			if !ok {
				rollback()
				return Result{}, ErrNotFound
			}
			undos = append(undos, undo{id: op.ID, existed: true, prev: prev})
			delete(q.items, op.ID)
		}
	}

	if len(q.items) > q.max {
		rollback()
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		q.gen++
	}
	return Result{Generation: q.gen, Revision: q.nextRev - 1}, nil
}

// Pop 原子地选取并删除 ReadyAt <= now 的任务，按规范顺序返回至多 limit 个。
func (q *Queue) Pop(now int64, limit int) ([]Item, error) {
	if now < 0 || limit <= 0 {
		return nil, ErrInvalidInput
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if now < q.now {
		return nil, ErrTime
	}
	q.now = now

	ready := make([]Item, 0, len(q.items))
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

// Snapshot 返回与内部状态隔离的一致快照，Items 按规范顺序排序。
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()

	items := make([]Item, 0, len(q.items))
	for _, it := range q.items {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return less(items[i], items[j]) })
	return Snapshot{
		Generation:   q.gen,
		NextRevision: q.nextRev,
		Now:          q.now,
		Items:        items,
	}
}
