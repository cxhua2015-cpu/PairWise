package expirytable424

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

// Table is a concurrency-safe in-memory expiry state table.
type Table struct {
	mu          sync.RWMutex
	maxEntries  int
	maxKeyBytes int
	now         int64
	generation  uint64
	nextRev     uint64
	entries     map[string]Entry
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		maxEntries:  o.MaxEntries,
		maxKeyBytes: o.MaxKeyBytes,
		nextRev:     1,
		entries:     make(map[string]Entry),
	}, nil
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool { return es[i].Key < es[j].Key })
}

// applyLocked runs the full transactional semantics on a candidate state and
// commits only on success. The caller must hold the write lock.
func (t *Table) applyLocked(b Batch) (Result, error) {
	if err := validateBatch(b, t.maxKeyBytes); err != nil {
		return Result{}, err
	}
	if b.Now < t.now {
		return Result{}, ErrTime
	}
	candidate := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt > b.Now {
			candidate[k] = e
		}
	}
	next := t.nextRev
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			candidate[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: next}
			next++
		case Touch:
			e, ok := candidate[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = next
			next++
			candidate[op.Key] = e
		case Delete:
			if _, ok := candidate[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Key)
		}
	}
	if len(candidate) > t.maxEntries {
		return Result{}, ErrCapacity
	}
	t.entries = candidate
	t.now = b.Now
	t.nextRev = next
	if len(b.Ops) > 0 {
		t.generation++
	}
	return Result{Generation: t.generation, Revision: next - 1}, nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.applyLocked(b)
}

func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < t.now {
		return nil, ErrTime
	}
	t.now = now
	var gone []Entry
	for k, e := range t.entries {
		if e.ExpiresAt <= now {
			gone = append(gone, e)
			delete(t.entries, k)
		}
	}
	sortEntries(gone)
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	es := make([]Entry, 0, len(t.entries))
	for _, e := range t.entries {
		es = append(es, e)
	}
	sortEntries(es)
	return Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRev,
		Now:          t.now,
		Entries:      es,
	}
}
