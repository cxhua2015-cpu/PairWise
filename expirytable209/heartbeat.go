package expirytable209

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
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Put Kind = iota + 1
	Touch
	Delete
)

type Options struct{ MaxEntries, MaxKeyBytes int }
type Op struct {
	Kind      Kind
	Key       string
	ExpiresAt int64
}
type Batch struct {
	Now int64
	Ops []Op
}
type Entry struct {
	Key       string
	ExpiresAt int64
	Revision  uint64
}
type Result struct{ Generation, Revision uint64 }
type Snapshot struct {
	Generation, NextRevision uint64
	Now                      int64
	Entries                  []Entry
}

// expiryHeap is a min-heap ordered by ExpiresAt used as an expiry index.
// Entries are invalidated lazily: a heap item is stale when the live entry
// no longer exists or its ExpiresAt differs.
type expiryItem struct {
	expiresAt int64
	key       string
}
type expiryHeap []expiryItem

func (h expiryHeap) Len() int           { return len(h) }
func (h expiryHeap) Less(i, j int) bool { return h[i].expiresAt < h[j].expiresAt }
func (h expiryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *expiryHeap) Push(x any)        { *h = append(*h, x.(expiryItem)) }
func (h *expiryHeap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	*h = old[:n-1]
	return it
}

type Table struct {
	mu          sync.Mutex
	maxEntries  int
	maxKeyBytes int
	entries     map[string]Entry
	idx         expiryHeap
	now         int64
	gen         uint64
	nextRev     uint64
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		maxEntries:  o.MaxEntries,
		maxKeyBytes: o.MaxKeyBytes,
		entries:     make(map[string]Entry),
		nextRev:     1,
	}, nil
}

func validKey(k string, maxBytes int) bool {
	if len(k) == 0 || len(k) > maxBytes {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validateBatch performs full structural validation before any state is read.
func (t *Table) validateBatch(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch, Delete:
		default:
			return ErrInvalidInput
		}
		if !validKey(op.Key, t.maxKeyBytes) {
			return ErrInvalidInput
		}
		if op.ExpiresAt < 0 {
			return ErrInvalidInput
		}
	}
	return nil
}

// evictLocked removes all live entries with ExpiresAt <= now from the
// candidate maps. It only touches the candidate copies.
func evict(entries map[string]Entry, idx *expiryHeap, now int64) {
	for len(*idx) > 0 && (*idx)[0].expiresAt <= now {
		it := heap.Pop(idx).(expiryItem)
		if e, ok := entries[it.key]; ok && e.ExpiresAt == it.expiresAt {
			delete(entries, it.key)
		}
	}
}

func (t *Table) Apply(b Batch) (Result, error) {
	if err := t.validateBatch(b); err != nil {
		return Result{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b.Now < t.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		return Result{Generation: t.gen, Revision: t.nextRev - 1}, nil
	}
	// Candidate transaction: work on copies so any failure rolls back
	// evictions, time and revisions together.
	entries := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		entries[k] = e
	}
	idx := make(expiryHeap, len(t.idx))
	copy(idx, t.idx)

	evict(entries, &idx, b.Now)

	rev := t.nextRev
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			e := Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: rev}
			entries[op.Key] = e
			heap.Push(&idx, expiryItem{expiresAt: op.ExpiresAt, key: op.Key})
			rev++
		case Touch:
			e, ok := entries[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = rev
			entries[op.Key] = e
			heap.Push(&idx, expiryItem{expiresAt: op.ExpiresAt, key: op.Key})
			rev++
		case Delete:
			if _, ok := entries[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(entries, op.Key)
		}
	}
	if len(entries) > t.maxEntries {
		return Result{}, ErrCapacity
	}
	t.entries = entries
	t.idx = idx
	t.now = b.Now
	t.nextRev = rev
	t.gen++
	return Result{Generation: t.gen, Revision: rev - 1}, nil
}

func (t *Table) Expire(now int64) ([]Entry, error) {
	if now < 0 {
		return nil, ErrInvalidInput
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < t.now {
		return nil, ErrTime
	}
	var gone []Entry
	for len(t.idx) > 0 && t.idx[0].expiresAt <= now {
		it := heap.Pop(&t.idx).(expiryItem)
		if e, ok := t.entries[it.key]; ok && e.ExpiresAt == it.expiresAt {
			delete(t.entries, it.key)
			gone = append(gone, e)
		}
	}
	t.now = now
	sort.Slice(gone, func(i, j int) bool { return gone[i].Key < gone[j].Key })
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make([]Entry, 0, len(t.entries))
	for _, e := range t.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return Snapshot{
		Generation:   t.gen,
		NextRevision: t.nextRev,
		Now:          t.now,
		Entries:      entries,
	}
}
