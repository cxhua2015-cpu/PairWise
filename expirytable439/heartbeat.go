package expirytable439

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
type Table struct {
	mu           sync.Mutex
	opts         Options
	entries      map[string]Entry
	generation   uint64
	nextRevision uint64
	now          int64
}

func New(opts Options) (*Table, error) {
	if opts.MaxEntries <= 0 || opts.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{opts: opts, entries: make(map[string]Entry), nextRevision: 1}, nil
}

// applyBatch executes the full candidate-transaction semantics on t.
// Callers must hold t.mu (or own t exclusively). On any error t is untouched.
func (t *Table) applyBatch(b Batch) (Result, error) {
	if err := t.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if b.Now < t.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		return Result{Generation: t.generation, Revision: t.nextRevision - 1}, nil
	}
	candidate := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt <= b.Now {
			continue
		}
		candidate[k] = e
	}
	rev := t.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			candidate[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: rev}
			rev++
		case Touch:
			e, ok := candidate[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = rev
			rev++
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
	t.nextRevision = rev
	t.generation++
	return Result{Generation: t.generation, Revision: rev - 1}, nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.applyBatch(b)
}

func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < t.now {
		return nil, ErrTime
	}
	t.now = now
	var gone []Entry
	for k, e := range t.entries {
		if e.ExpiresAt <= now {
			gone = append(gone, e)
			delete(t.entries, k)
		}
	}
	if len(gone) > 0 {
		t.generation++
		sort.Slice(gone, func(i, j int) bool { return gone[i].Key < gone[j].Key })
	}
	return gone, nil
}

// snapshotLocked returns a copy of the current state; caller must hold t.mu.
func (t *Table) snapshotLocked() Snapshot {
	entries := make([]Entry, 0, len(t.entries))
	for _, e := range t.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRevision,
		Now:          t.now,
		Entries:      entries,
	}
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked()
}
