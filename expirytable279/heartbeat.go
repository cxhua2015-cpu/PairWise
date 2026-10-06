package expirytable279

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

// Table is a concurrency-safe in-memory expiry state table driven by
// explicit non-negative monotonic time.
type Table struct {
	mu           sync.Mutex
	maxEntries   int
	maxKeyBytes  int
	now          int64
	generation   uint64
	nextRevision uint64
	entries      map[string]Entry
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		maxEntries:   o.MaxEntries,
		maxKeyBytes:  o.MaxKeyBytes,
		nextRevision: 1,
		entries:      make(map[string]Entry),
	}, nil
}

// Apply validates the batch, checks monotonic time, evicts entries with
// ExpiresAt <= Now on a candidate copy, then applies ops in order.
// Any failure rolls back evictions, time, and revisions.
func (t *Table) Apply(b Batch) (Result, error) {
	if err := t.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b.Now < t.now {
		return Result{}, ErrTime
	}

	cand := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		cand[k] = e
	}
	for k, e := range cand {
		if e.ExpiresAt <= b.Now {
			delete(cand, k)
		}
	}

	nextRev := t.nextRevision
	var lastRev uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			cand[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: nextRev}
			lastRev = nextRev
			nextRev++
		case Touch:
			e, ok := cand[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = nextRev
			cand[op.Key] = e
			lastRev = nextRev
			nextRev++
		case Delete:
			if _, ok := cand[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Key)
		}
	}
	if len(cand) > t.maxEntries {
		return Result{}, ErrCapacity
	}

	t.entries = cand
	t.now = b.Now
	t.nextRevision = nextRev
	if len(b.Ops) > 0 {
		t.generation++
	}
	return Result{Generation: t.generation, Revision: lastRev}, nil
}

// Expire removes and returns all entries with ExpiresAt <= now, advancing
// the logical clock. It uses the same closed-boundary semantics as Apply.
func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < t.now {
		return nil, ErrTime
	}
	var gone []Entry
	for k, e := range t.entries {
		if e.ExpiresAt <= now {
			gone = append(gone, e)
			delete(t.entries, k)
		}
	}
	sort.Slice(gone, func(i, j int) bool { return gone[i].Key < gone[j].Key })
	t.now = now
	return gone, nil
}

// Snapshot returns an isolated copy of the current state; entries are
// sorted by key in canonical order.
func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRevision,
		Now:          t.now,
		Entries:      t.sortedEntries(),
	}
}

// sortedEntries requires t.mu to be held.
func (t *Table) sortedEntries() []Entry {
	es := make([]Entry, 0, len(t.entries))
	for _, e := range t.entries {
		es = append(es, e)
	}
	sort.Slice(es, func(i, j int) bool { return es[i].Key < es[j].Key })
	return es
}
