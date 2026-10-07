package expirytable329

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

type entry struct {
	expiresAt int64
	revision  uint64
}

type heapItem struct {
	expiresAt int64
	revision  uint64
	key       string
}

type expiryHeap []heapItem

func (h expiryHeap) Len() int { return len(h) }
func (h expiryHeap) Less(i, j int) bool {
	if h[i].expiresAt != h[j].expiresAt {
		return h[i].expiresAt < h[j].expiresAt
	}
	return h[i].key < h[j].key
}
func (h expiryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *expiryHeap) Push(x any)        { *h = append(*h, x.(heapItem)) }
func (h *expiryHeap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	*h = old[:n-1]
	return it
}

type Table struct {
	mu           sync.Mutex
	maxEntries   int
	maxKeyBytes  int
	entries      map[string]entry
	idx          expiryHeap
	now          int64
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		maxEntries:   o.MaxEntries,
		maxKeyBytes:  o.MaxKeyBytes,
		entries:      make(map[string]entry),
		nextRevision: 1,
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

func validateOp(op Op, maxKeyBytes int) error {
	switch op.Kind {
	case Put, Touch:
		if !validKey(op.Key, maxKeyBytes) || op.ExpiresAt < 0 {
			return ErrInvalidInput
		}
	case Delete:
		if !validKey(op.Key, maxKeyBytes) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// expireLocked removes every entry with ExpiresAt <= now from the candidate
// state. Stale heap items (superseded revisions) are discarded lazily.
func (t *Table) expireLocked(entries map[string]entry, idx *expiryHeap, now int64) []Entry {
	var gone []Entry
	for len(*idx) > 0 && (*idx)[0].expiresAt <= now {
		it := heap.Pop(idx).(heapItem)
		cur, ok := entries[it.key]
		if !ok || cur.revision != it.revision {
			continue
		}
		delete(entries, it.key)
		gone = append(gone, Entry{Key: it.key, ExpiresAt: it.expiresAt, Revision: it.revision})
	}
	return gone
}

func (t *Table) Apply(b Batch) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Phase 1: full structural validation before touching any state.
	for _, op := range b.Ops {
		if err := validateOp(op, t.maxKeyBytes); err != nil {
			return Result{}, err
		}
	}
	// Phase 2: time check (explicit, non-negative, monotonic).
	if b.Now < 0 || b.Now < t.now {
		return Result{}, ErrTime
	}

	// Candidate transaction: work on copies so any failure rolls back
	// expirations, time, revisions and generation together.
	entries := make(map[string]entry, len(t.entries))
	for k, v := range t.entries {
		entries[k] = v
	}
	idx := make(expiryHeap, len(t.idx))
	copy(idx, t.idx)

	t.expireLocked(entries, &idx, b.Now)

	revision := t.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			e := entry{expiresAt: op.ExpiresAt, revision: revision}
			entries[op.Key] = e
			heap.Push(&idx, heapItem{expiresAt: e.expiresAt, revision: e.revision, key: op.Key})
			revision++
		case Touch:
			e, ok := entries[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.expiresAt = op.ExpiresAt
			e.revision = revision
			entries[op.Key] = e
			heap.Push(&idx, heapItem{expiresAt: e.expiresAt, revision: e.revision, key: op.Key})
			revision++
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

	// Commit.
	t.entries = entries
	t.idx = idx
	t.now = b.Now
	t.nextRevision = revision
	if len(b.Ops) > 0 {
		t.generation++
	}
	return Result{Generation: t.generation, Revision: revision - 1}, nil
}

func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < 0 || now < t.now {
		return nil, ErrTime
	}
	t.now = now
	return t.expireLocked(t.entries, &t.idx, now), nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make([]Entry, 0, len(t.entries))
	for k, e := range t.entries {
		entries = append(entries, Entry{Key: k, ExpiresAt: e.expiresAt, Revision: e.revision})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRevision,
		Now:          t.now,
		Entries:      entries,
	}
}
