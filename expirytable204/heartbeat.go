package expirytable204

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

// heapItem is one node of the lazy expiry min-heap. Revision identifies the
// entry version the node refers to; nodes whose revision no longer matches
// the live entry are stale and skipped on pop.
type heapItem struct {
	expiresAt int64
	revision  uint64
	key       string
}

type expiryHeap []heapItem

func (h expiryHeap) less(i, j int) bool {
	if h[i].expiresAt != h[j].expiresAt {
		return h[i].expiresAt < h[j].expiresAt
	}
	return h[i].key < h[j].key
}

func (h *expiryHeap) push(it heapItem) {
	*h = append(*h, it)
	for i := len(*h) - 1; i > 0; {
		p := (i - 1) / 2
		if !h.less(i, p) {
			break
		}
		(*h)[i], (*h)[p] = (*h)[p], (*h)[i]
		i = p
	}
}

func (h *expiryHeap) pop() heapItem {
	old := *h
	top := old[0]
	last := old[len(old)-1]
	old = old[:len(old)-1]
	if len(old) > 0 {
		old[0] = last
		for i := 0; ; {
			child := 2*i + 1
			if child >= len(old) {
				break
			}
			if child+1 < len(old) && old.less(child+1, child) {
				child++
			}
			if !old.less(child, i) {
				break
			}
			old[child], old[i] = old[i], old[child]
			i = child
		}
	}
	*h = old
	return top
}

func (h expiryHeap) clone() expiryHeap {
	c := make(expiryHeap, len(h))
	copy(c, h)
	return c
}

// collectExpired removes every live entry with ExpiresAt <= now from entries,
// discarding stale heap nodes, and returns the removed entries.
func collectExpired(entries map[string]Entry, h *expiryHeap, now int64) []Entry {
	var gone []Entry
	for len(*h) > 0 && (*h)[0].expiresAt <= now {
		it := h.pop()
		e, ok := entries[it.key]
		if !ok || e.Revision != it.revision {
			continue
		}
		delete(entries, it.key)
		gone = append(gone, e)
	}
	return gone
}

type Table struct {
	mu          sync.Mutex
	maxEntries  int
	maxKeyBytes int
	entries     map[string]Entry
	heap        expiryHeap
	generation  uint64
	nextRev     uint64
	lastRev     uint64
	now         int64
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
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (t *Table) validate(b Batch) error {
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch, Delete:
		default:
			return ErrInvalidInput
		}
		if !validKey(op.Key, t.maxKeyBytes) {
			return ErrInvalidInput
		}
	}
	return nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	if err := t.validate(b); err != nil {
		return Result{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b.Now < 0 || b.Now < t.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		return Result{Generation: t.generation, Revision: t.lastRev}, nil
	}

	entries := make(map[string]Entry, len(t.entries))
	for k, v := range t.entries {
		entries[k] = v
	}
	heap := t.heap.clone()
	collectExpired(entries, &heap, b.Now)

	nextRev, lastRev := t.nextRev, t.lastRev
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			e := Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: nextRev}
			nextRev++
			lastRev = e.Revision
			entries[op.Key] = e
			heap.push(heapItem{expiresAt: e.ExpiresAt, revision: e.Revision, key: e.Key})
		case Touch:
			e, ok := entries[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = nextRev
			nextRev++
			lastRev = e.Revision
			entries[op.Key] = e
			heap.push(heapItem{expiresAt: e.ExpiresAt, revision: e.Revision, key: e.Key})
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
	t.heap = heap
	t.now = b.Now
	t.nextRev = nextRev
	t.lastRev = lastRev
	t.generation++
	return Result{Generation: t.generation, Revision: lastRev}, nil
}

func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < 0 || now < t.now {
		return nil, ErrTime
	}
	gone := collectExpired(t.entries, &t.heap, now)
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
		Generation:   t.generation,
		NextRevision: t.nextRev,
		Now:          t.now,
		Entries:      entries,
	}
}
