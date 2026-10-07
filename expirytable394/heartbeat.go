package expirytable394

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
// Internally it keeps a single map index keyed by entry key. All
// mutations run as candidate transactions: Apply validates the batch,
// clones the index, expires and applies ops on the clone, and only
// commits (swapping the index, time and revision counter) when the
// whole batch succeeds. Any error discards the candidate, so
// evictions, time and revisions roll back together.
type Table struct {
	mu           sync.Mutex
	maxEntries   int
	maxKeyBytes  int
	entries      map[string]Entry
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
		entries:      make(map[string]Entry),
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

// validateOp reports whether op is structurally valid.
func (t *Table) validateOp(op Op) bool {
	switch op.Kind {
	case Put, Touch, Delete:
	default:
		return false
	}
	return validKey(op.Key, t.maxKeyBytes)
}

func (t *Table) Apply(b Batch) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 1. Full structural validation before reading any state.
	for _, op := range b.Ops {
		if !t.validateOp(op) {
			return Result{}, ErrInvalidInput
		}
	}
	// 2. Time check: explicit, non-negative, monotonic.
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	if b.Now < t.now {
		return Result{}, ErrTime
	}
	// Empty batches change nothing (generation included).
	if len(b.Ops) == 0 {
		return Result{Generation: t.generation, Revision: t.lastRevision()}, nil
	}

	// 3. Candidate transaction on a cloned index.
	cand := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt <= b.Now {
			continue // expire closed-interval boundary first
		}
		cand[k] = e
	}
	nextRev := t.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			cand[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: nextRev}
			nextRev++
		case Touch:
			e, ok := cand[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = nextRev
			nextRev++
			cand[op.Key] = e
		case Delete:
			if _, ok := cand[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Key)
		}
	}
	// 4. Final capacity check; failure rolls back everything.
	if len(cand) > t.maxEntries {
		return Result{}, ErrCapacity
	}

	// 5. Commit.
	t.entries = cand
	t.now = b.Now
	t.nextRevision = nextRev
	t.generation++
	return Result{Generation: t.generation, Revision: nextRev - 1}, nil
}

func (t *Table) lastRevision() uint64 { return t.nextRevision - 1 }

func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < 0 {
		return nil, ErrInvalidInput
	}
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
	sortEntries(gone)
	t.now = now
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	var entries []Entry
	for _, e := range t.entries {
		entries = append(entries, e)
	}
	sortEntries(entries)
	return Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRevision,
		Now:          t.now,
		Entries:      entries,
	}
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool { return es[i].Key < es[j].Key })
}
