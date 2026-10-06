package expirytable269

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

// Table is a concurrency-safe in-memory expiry state table. The primary
// index is a hash map keyed by Entry.Key; snapshots are sorted by key for
// deterministic output.
type Table struct {
	mu      sync.Mutex
	opts    Options
	now     int64
	gen     uint64
	nextRev uint64
	entries map[string]Entry
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{opts: o, nextRev: 1, entries: make(map[string]Entry)}, nil
}

// Apply validates the batch structurally, checks the monotonic clock, then
// runs a candidate transaction: entries with ExpiresAt <= Now are evicted
// first, then Put/Touch/Delete execute in order. Any failure rolls back
// evictions, time, and revisions together.
func (t *Table) Apply(b Batch) (Result, error) {
	if err := t.ValidateBatch(b); err != nil {
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
	candidate := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt > b.Now {
			candidate[k] = e
		}
	}
	nextRev := t.nextRev
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			candidate[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: nextRev}
			nextRev++
		case Touch:
			e, ok := candidate[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = nextRev
			nextRev++
			candidate[op.Key] = e
		case Delete:
			if _, ok := candidate[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Key)
		}
	}
	if len(candidate) > t.opts.MaxEntries {
		return Result{}, ErrCapacity
	}
	t.entries = candidate
	t.now = b.Now
	t.gen++
	t.nextRev = nextRev
	return Result{Generation: t.gen, Revision: nextRev - 1}, nil
}

// Expire removes and returns all entries with ExpiresAt <= now, using the
// same closed-interval boundary as Apply, and advances the logical clock.
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
	for k, e := range t.entries {
		if e.ExpiresAt <= now {
			gone = append(gone, e)
			delete(t.entries, k)
		}
	}
	sortEntries(gone)
	t.now = now
	if len(gone) > 0 {
		t.gen++
	}
	return gone, nil
}

// Snapshot returns a deterministic, fully detached copy of the state.
func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make([]Entry, 0, len(t.entries))
	for _, e := range t.entries {
		entries = append(entries, e)
	}
	sortEntries(entries)
	return Snapshot{
		Generation:   t.gen,
		NextRevision: t.nextRev,
		Now:          t.now,
		Entries:      entries,
	}
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
}
