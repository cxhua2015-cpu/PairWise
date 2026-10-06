package expirytable249

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
// The zero value is not usable; construct with New.
type Table struct {
	mu           sync.RWMutex
	opts         Options
	entries      map[string]Entry
	generation   uint64
	nextRevision uint64
	now          int64
}

func New(o Options) (*Table, error) {
	if err := validateOptions(o); err != nil {
		return nil, err
	}
	return &Table{
		opts:         o,
		entries:      make(map[string]Entry),
		nextRevision: 1,
	}, nil
}

// Apply validates the batch, checks monotonic time, then runs a candidate
// transaction: entries with ExpiresAt <= Now are evicted first, then ops run
// in order. Any failure rolls back evictions, time and revisions.
func (t *Table) Apply(b Batch) (Result, error) {
	if err := t.ValidateBatch(b); err != nil {
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
	var lastRevision uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			lastRevision = nextRevision
			nextRevision++
			candidate[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: lastRevision}
		case Touch:
			e, ok := candidate[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			lastRevision = nextRevision
			nextRevision++
			e.ExpiresAt = op.ExpiresAt
			e.Revision = lastRevision
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
	t.nextRevision = nextRevision
	if len(b.Ops) > 0 {
		t.generation++
	}
	return Result{Generation: t.generation, Revision: lastRevision}, nil
}

// Expire removes and returns all entries with ExpiresAt <= now, advancing
// the logical clock. The boundary is the same closed interval used by Apply.
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
	t.now = now
	sort.Slice(gone, func(i, j int) bool { return gone[i].Key < gone[j].Key })
	return gone, nil
}

// Snapshot returns a consistent view isolated from internal state.
func (t *Table) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.snapshotLocked()
}

func (t *Table) snapshotLocked() Snapshot {
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
