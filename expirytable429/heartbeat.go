package expirytable429

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
// The zero Table is not usable; construct one with New.
type Table struct {
	mu    sync.RWMutex
	opts  Options
	state tableState
}

// tableState holds the mutable logical state of a Table.
type tableState struct {
	now          int64
	generation   uint64
	nextRevision uint64
	entries      map[string]Entry
}

// New creates a Table. Capacity and length limits must be positive.
func New(opts Options) (*Table, error) {
	if opts.MaxEntries <= 0 || opts.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		opts: opts,
		state: tableState{
			nextRevision: 1,
			entries:      make(map[string]Entry),
		},
	}, nil
}

// Apply validates the batch, then commits it transactionally:
// entries with ExpiresAt <= Now are evicted first, then ops run in order.
// Any failure rolls back evictions, time, and revisions together.
func (t *Table) Apply(b Batch) (Result, error) {
	if err := t.validateBatch(b); err != nil {
		return Result{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state.apply(b, t.opts)
}

// apply commits an already structurally validated batch against s.
func (s *tableState) apply(b Batch, opts Options) (Result, error) {
	if b.Now < s.now {
		return Result{}, ErrTime
	}
	candidate := make(map[string]Entry, len(s.entries))
	for k, e := range s.entries {
		candidate[k] = e
	}
	for k, e := range candidate {
		if e.ExpiresAt <= b.Now {
			delete(candidate, k)
		}
	}
	nextRevision := s.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			candidate[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: nextRevision}
			nextRevision++
		case Touch:
			e, ok := candidate[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = nextRevision
			nextRevision++
			candidate[op.Key] = e
		case Delete:
			if _, ok := candidate[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Key)
		}
	}
	if len(candidate) > opts.MaxEntries {
		return Result{}, ErrCapacity
	}
	s.entries = candidate
	s.now = b.Now
	s.nextRevision = nextRevision
	if len(b.Ops) > 0 {
		s.generation++
	}
	return Result{Generation: s.generation, Revision: nextRevision - 1}, nil
}

// Expire removes and returns all entries with ExpiresAt <= now,
// advancing the logical clock. Entries are returned sorted by key.
func (t *Table) Expire(now int64) ([]Entry, error) {
	if now < 0 {
		return nil, ErrInvalidInput
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < t.state.now {
		return nil, ErrTime
	}
	var gone []Entry
	for k, e := range t.state.entries {
		if e.ExpiresAt <= now {
			delete(t.state.entries, k)
			gone = append(gone, e)
		}
	}
	t.state.now = now
	sort.Slice(gone, func(i, j int) bool { return gone[i].Key < gone[j].Key })
	return gone, nil
}

// Snapshot returns a consistent copy of the current state.
// Entries are sorted by key and isolated from internal state.
func (t *Table) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.state.snapshot()
}

func (s *tableState) snapshot() Snapshot {
	entries := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return Snapshot{
		Generation:   s.generation,
		NextRevision: s.nextRevision,
		Now:          s.now,
		Entries:      entries,
	}
}
