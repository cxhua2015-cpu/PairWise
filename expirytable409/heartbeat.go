package expirytable409

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

// Apply validates the batch structurally, checks monotonic time, then runs
// expiry (ExpiresAt <= Now) and the ops on a candidate state. Any error
// rolls back evictions, time, and revisions together.
func (t *Table) Apply(b Batch) (Result, error) {
	if err := t.validateBatch(b); err != nil {
		return Result{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b.Now < t.now {
		return Result{}, ErrTime
	}
	candidate := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt > b.Now {
			candidate[k] = e
		}
	}
	nextRevision := t.nextRevision
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
			candidate[op.Key] = e
			nextRevision++
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
	t.nextRevision = nextRevision
	if len(b.Ops) > 0 {
		t.generation++
	}
	return Result{Generation: t.generation, Revision: nextRevision - 1}, nil
}

// Expire removes and returns all entries with ExpiresAt <= now, advancing
// the logical clock. It uses the same closed-interval boundary as Apply.
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
	sort.Slice(gone, func(i, j int) bool { return gone[i].Key < gone[j].Key })
	t.now = now
	return gone, nil
}

// Snapshot returns a consistent copy of the current state; the returned
// slice is fully isolated from internal state.
func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRevision,
		Now:          t.now,
		Entries:      make([]Entry, 0, len(t.entries)),
	}
	for _, e := range t.entries {
		s.Entries = append(s.Entries, e)
	}
	sort.Slice(s.Entries, func(i, j int) bool { return s.Entries[i].Key < s.Entries[j].Key })
	return s
}
