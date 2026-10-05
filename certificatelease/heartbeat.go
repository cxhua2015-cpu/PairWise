package certificatelease

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

// Table is a concurrency-safe in-memory certificate lease table.
//
// The table indexes live entries by key in a map; there is deliberately no
// secondary expiry index, so expiry sweeps are linear scans. All time is
// explicit, non-negative and supplied by the caller; the table enforces that
// it never moves backwards.
type Table struct {
	mu         sync.RWMutex
	opts       Options
	entries    map[string]Entry
	now        int64
	generation uint64
	revision   uint64 // last allocated revision
}

// New creates a table with positive capacity and key-length limits.
func New(opts Options) (*Table, error) {
	if opts.MaxEntries <= 0 || opts.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		opts:    opts,
		entries: make(map[string]Entry),
	}, nil
}

func validKey(key string, maxBytes int) bool {
	if len(key) == 0 || len(key) > maxBytes {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// validate performs the complete structural check of a batch without touching
// table state. State-dependent checks (time, existence, capacity) happen later.
func validate(b Batch, opts Options) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch, Delete:
		default:
			return ErrInvalidInput
		}
		if !validKey(op.Key, opts.MaxKeyBytes) {
			return ErrInvalidInput
		}
		if op.ExpiresAt < 0 {
			return ErrInvalidInput
		}
	}
	return nil
}

// Apply validates the batch structurally, enforces monotonic time, and then
// executes the batch against a private candidate: entries with
// ExpiresAt <= Now are swept first and the ops run in order. The live table is
// only replaced when the whole candidate succeeds, so evictions, time and
// revisions are rolled back together on any error.
func (t *Table) Apply(b Batch) (Result, error) {
	if err := validate(b, t.opts); err != nil {
		return Result{}, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if b.Now < t.now {
		return Result{}, ErrTime
	}

	// Build the candidate on a copy; t.entries stays untouched until commit.
	candidate := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		candidate[k] = e
	}
	nextRevision := t.revision

	// Candidate sweep: closed interval ExpiresAt <= Now.
	for k, e := range candidate {
		if e.ExpiresAt <= b.Now {
			delete(candidate, k)
		}
	}

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			nextRevision++
			candidate[op.Key] = Entry{
				Key:       op.Key,
				ExpiresAt: op.ExpiresAt,
				Revision:  nextRevision,
			}
		case Touch:
			e, ok := candidate[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			nextRevision++
			e.ExpiresAt = op.ExpiresAt
			e.Revision = nextRevision
			candidate[op.Key] = e
		case Delete:
			delete(candidate, op.Key)
		}
	}

	// Capacity is enforced on the final candidate only.
	if len(candidate) > t.opts.MaxEntries {
		return Result{}, ErrCapacity
	}

	if len(b.Ops) > 0 {
		t.generation++
	}
	t.entries = candidate
	t.now = b.Now
	t.revision = nextRevision
	return Result{Generation: t.generation, Revision: t.revision}, nil
}

// Expire removes every entry with ExpiresAt <= now using the same closed
// interval boundary as Apply's candidate sweep. The removed entries are
// returned as an independent, key-sorted slice.
func (t *Table) Expire(now int64) ([]Entry, error) {
	if now < 0 {
		return nil, ErrInvalidInput
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if now < t.now {
		return nil, ErrTime
	}

	var removed []Entry
	for k, e := range t.entries {
		if e.ExpiresAt <= now {
			removed = append(removed, e)
			delete(t.entries, k)
		}
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i].Key < removed[j].Key })
	t.now = now
	return removed, nil
}

// Snapshot returns an isolated, key-sorted copy of the table state.
func (t *Table) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	entries := make([]Entry, 0, len(t.entries))
	for _, e := range t.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return Snapshot{
		Generation:   t.generation,
		NextRevision: t.revision + 1,
		Now:          t.now,
		Entries:      entries,
	}
}
