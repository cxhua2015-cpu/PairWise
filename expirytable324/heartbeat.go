package expirytable324

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
//
// The primary index is a hash map keyed by Entry.Key. Mutations run as
// candidate transactions: Apply clones the index, evicts entries with
// ExpiresAt <= Now, replays the ops in order, and commits only if the
// final state satisfies the capacity limit. Any error discards the
// candidate, rolling back evictions, time and revisions together.
type Table struct {
	mu         sync.Mutex
	max        int
	maxKey     int
	now        int64
	generation uint64
	revision   uint64
	entries    map[string]Entry
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		max:     o.MaxEntries,
		maxKey:  o.MaxKeyBytes,
		entries: make(map[string]Entry),
	}, nil
}

func validKey(k string, max int) bool {
	if len(k) == 0 || len(k) > max {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validateOp reports whether a single op is structurally valid.
func (t *Table) validateOp(o Op) error {
	switch o.Kind {
	case Put, Touch, Delete:
	default:
		return ErrInvalidInput
	}
	if !validKey(o.Key, t.maxKey) {
		return ErrInvalidInput
	}
	return nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Phase 1: full structural validation before reading any state.
	for _, o := range b.Ops {
		if err := t.validateOp(o); err != nil {
			return Result{}, err
		}
	}
	// Phase 2: time validation (non-negative, monotone).
	if b.Now < 0 || b.Now < t.now {
		return Result{}, ErrTime
	}
	// Empty batches never mutate state.
	if len(b.Ops) == 0 {
		return Result{Generation: t.generation, Revision: t.revision}, nil
	}

	// Candidate transaction: clone the index so any failure rolls back
	// evictions, time and revisions together.
	cand := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt <= b.Now {
			continue // evict expired entries on the candidate
		}
		cand[k] = e
	}
	rev := t.revision
	for _, o := range b.Ops {
		switch o.Kind {
		case Put:
			rev++
			cand[o.Key] = Entry{Key: o.Key, ExpiresAt: o.ExpiresAt, Revision: rev}
		case Touch:
			e, ok := cand[o.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			rev++
			e.ExpiresAt = o.ExpiresAt
			e.Revision = rev
			cand[o.Key] = e
		case Delete:
			if _, ok := cand[o.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, o.Key)
		}
	}
	if len(cand) > t.max {
		return Result{}, ErrCapacity
	}

	// Commit.
	t.entries = cand
	t.now = b.Now
	t.revision = rev
	t.generation++
	return Result{Generation: t.generation, Revision: t.revision}, nil
}

func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < 0 || now < t.now {
		return nil, ErrTime
	}
	var gone []Entry
	for k, e := range t.entries {
		if e.ExpiresAt <= now {
			gone = append(gone, e)
			delete(t.entries, k)
		}
	}
	t.now = now
	sort.Slice(gone, func(i, j int) bool { return gone[i].Key < gone[j].Key })
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Snapshot{
		Generation:   t.generation,
		NextRevision: t.revision + 1,
		Now:          t.now,
	}
	if len(t.entries) > 0 {
		s.Entries = make([]Entry, 0, len(t.entries))
		for _, e := range t.entries {
			s.Entries = append(s.Entries, e)
		}
		sort.Slice(s.Entries, func(i, j int) bool { return s.Entries[i].Key < s.Entries[j].Key })
	}
	return s
}
