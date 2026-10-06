package expirytable244

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

// Table is a concurrent-safe in-memory expiry state table.
//
// State is stored as an insertion-ordered key list plus a hash index, so
// lookups are O(1) and snapshots iterate in stable insertion order.
type Table struct {
	mu           sync.RWMutex
	maxEntries   int
	maxKeyBytes  int
	now          int64
	generation   uint64
	nextRevision uint64
	order        []string
	items        map[string]Entry
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		maxEntries:   o.MaxEntries,
		maxKeyBytes:  o.MaxKeyBytes,
		nextRevision: 1,
		items:        make(map[string]Entry),
	}, nil
}

// Apply validates the batch, checks monotonic time, then runs a candidate
// transaction: sweep entries with ExpiresAt <= Now, apply ops in order, and
// verify final capacity. Any error rolls back sweeps, time and revisions.
func (t *Table) Apply(b Batch) (Result, error) {
	if err := t.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b.Now < t.now {
		return Result{}, ErrTime
	}

	order := make([]string, 0, len(t.order))
	items := make(map[string]Entry, len(t.items))
	for _, k := range t.order {
		e := t.items[k]
		if e.ExpiresAt <= b.Now {
			continue
		}
		order = append(order, k)
		items[k] = e
	}

	nextRev := t.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if _, ok := items[op.Key]; !ok {
				order = append(order, op.Key)
			}
			items[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: nextRev}
			nextRev++
		case Touch:
			e, ok := items[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = nextRev
			items[op.Key] = e
			nextRev++
		case Delete:
			if _, ok := items[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(items, op.Key)
			for i, k := range order {
				if k == op.Key {
					order = append(order[:i], order[i+1:]...)
					break
				}
			}
		}
	}
	if len(items) > t.maxEntries {
		return Result{}, ErrCapacity
	}

	t.now = b.Now
	t.order = order
	t.items = items
	t.nextRevision = nextRev
	if len(b.Ops) > 0 {
		t.generation++
	}
	return Result{Generation: t.generation, Revision: nextRev - 1}, nil
}

// Expire advances the clock and removes entries with ExpiresAt <= now,
// returning them in insertion order.
func (t *Table) Expire(now int64) ([]Entry, error) {
	if now < 0 {
		return nil, ErrInvalidInput
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < t.now {
		return nil, ErrTime
	}
	t.now = now
	var gone []Entry
	kept := t.order[:0]
	for _, k := range t.order {
		e := t.items[k]
		if e.ExpiresAt <= now {
			gone = append(gone, e)
			delete(t.items, k)
			continue
		}
		kept = append(kept, k)
	}
	t.order = kept
	return gone, nil
}

// Snapshot returns a copy of the current state, isolated from the table.
func (t *Table) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s := Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRevision,
		Now:          t.now,
		Entries:      make([]Entry, 0, len(t.order)),
	}
	for _, k := range t.order {
		s.Entries = append(s.Entries, t.items[k])
	}
	return s
}
