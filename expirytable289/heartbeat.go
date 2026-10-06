package expirytable289

import (
	"errors"
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

// Table is a concurrency-safe in-memory expiry state table. Entries are
// kept in insertion order in order, with index providing O(1) key lookup.
type Table struct {
	mu           sync.Mutex
	maxEntries   int
	maxKeyBytes  int
	now          int64
	generation   uint64
	nextRevision uint64
	order        []string
	index        map[string]Entry
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		maxEntries:   o.MaxEntries,
		maxKeyBytes:  o.MaxKeyBytes,
		nextRevision: 1,
		index:        make(map[string]Entry),
	}, nil
}

// Apply validates the batch, then runs it against a candidate state:
// first evicting entries with ExpiresAt <= Now, then applying ops in
// order. Any failure rolls back evictions, time and revisions.
func (t *Table) Apply(b Batch) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.validateBatchLocked(b); err != nil {
		return Result{}, err
	}
	if b.Now < 0 || b.Now < t.now {
		return Result{}, ErrTime
	}

	order := make([]string, len(t.order))
	copy(order, t.order)
	index := make(map[string]Entry, len(t.index))
	for k, e := range t.index {
		index[k] = e
	}
	nextRevision := t.nextRevision

	// Candidate eviction: closed interval ExpiresAt <= Now.
	kept := order[:0]
	for _, k := range order {
		if index[k].ExpiresAt <= b.Now {
			delete(index, k)
		} else {
			kept = append(kept, k)
		}
	}
	order = kept

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if _, ok := index[op.Key]; !ok {
				order = append(order, op.Key)
			}
			index[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: nextRevision}
			nextRevision++
		case Touch:
			e, ok := index[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = nextRevision
			index[op.Key] = e
			nextRevision++
		case Delete:
			if _, ok := index[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(index, op.Key)
			for i, k := range order {
				if k == op.Key {
					order = append(order[:i], order[i+1:]...)
					break
				}
			}
		}
	}

	if len(index) > t.maxEntries {
		return Result{}, ErrCapacity
	}

	t.order = order
	t.index = index
	t.now = b.Now
	t.nextRevision = nextRevision
	if len(b.Ops) > 0 {
		t.generation++
	}
	return Result{Generation: t.generation, Revision: nextRevision - 1}, nil
}

// Expire advances time and removes entries with ExpiresAt <= now,
// returning them in insertion order.
func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < 0 || now < t.now {
		return nil, ErrTime
	}
	var gone []Entry
	kept := t.order[:0]
	for _, k := range t.order {
		e := t.index[k]
		if e.ExpiresAt <= now {
			delete(t.index, k)
			gone = append(gone, e)
		} else {
			kept = append(kept, k)
		}
	}
	t.order = kept
	t.now = now
	return gone, nil
}

// Snapshot returns a copy of the current state, isolated from the table.
func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRevision,
		Now:          t.now,
		Entries:      make([]Entry, 0, len(t.order)),
	}
	for _, k := range t.order {
		s.Entries = append(s.Entries, t.index[k])
	}
	return s
}
