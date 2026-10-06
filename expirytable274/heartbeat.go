package expirytable274

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

// Table is a concurrent-safe in-memory expiry state table.
type Table struct {
	mu           sync.RWMutex
	opts         Options
	entries      map[string]Entry
	now          int64
	generation   uint64
	nextRevision uint64
}

func New(opts Options) (*Table, error) {
	if opts.MaxEntries <= 0 || opts.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		opts:         opts,
		entries:      make(map[string]Entry),
		nextRevision: 1,
	}, nil
}

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
		return Result{}, nil
	}
	// Candidate state: start from a copy, evict ExpiresAt <= Now, then apply ops.
	cand := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt <= b.Now {
			continue
		}
		cand[k] = e
	}
	revision := t.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			cand[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: revision}
			revision++
		case Touch:
			if _, ok := cand[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			cand[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: revision}
			revision++
		case Delete:
			if _, ok := cand[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Key)
		}
	}
	if len(cand) > t.opts.MaxEntries {
		return Result{}, ErrCapacity
	}
	// Commit.
	t.entries = cand
	t.now = b.Now
	t.generation++
	t.nextRevision = revision
	return Result{Generation: t.generation, Revision: revision - 1}, nil
}

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

func (t *Table) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.snapshotLocked()
}

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
