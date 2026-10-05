package resourcelease169

import (
	"errors"
	"maps"
	"slices"
	"strings"
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

// Table is a concurrency-safe in-memory resource lease table.
// The zero Table is not usable; construct it with New.
type Table struct {
	mu           sync.Mutex
	entries      map[string]Entry
	now          int64
	generation   uint64
	nextRevision uint64
	maxEntries   int
	maxKeyBytes  int
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		entries:      make(map[string]Entry),
		nextRevision: 1,
		maxEntries:   o.MaxEntries,
		maxKeyBytes:  o.MaxKeyBytes,
	}, nil
}

func validKey(key string, maxBytes int) bool {
	if len(key) == 0 || len(key) > maxBytes {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (t *Table) Apply(b Batch) (Result, error) {
	// Structural validation of the whole batch happens before any
	// state is read or any time check is performed.
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch, Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validKey(op.Key, t.maxKeyBytes) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind != Delete && op.ExpiresAt < 0 {
			return Result{}, ErrInvalidInput
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if b.Now < t.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		return Result{Generation: t.generation, Revision: t.nextRevision - 1}, nil
	}

	// Candidate transaction: work on a copy so any failure rolls back
	// expirations, time, and revision together.
	cand := maps.Clone(t.entries)
	for k, e := range cand {
		if e.ExpiresAt <= b.Now {
			delete(cand, k)
		}
	}
	rev := t.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			cand[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: rev}
			rev++
		case Touch:
			e, ok := cand[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = rev
			rev++
			cand[op.Key] = e
		case Delete:
			if _, ok := cand[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Key)
		}
	}
	if len(cand) > t.maxEntries {
		return Result{}, ErrCapacity
	}

	t.entries = cand
	t.now = b.Now
	t.nextRevision = rev
	t.generation++
	return Result{Generation: t.generation, Revision: rev - 1}, nil
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
			delete(t.entries, k)
			gone = append(gone, e)
		}
	}
	slices.SortFunc(gone, func(a, b Entry) int {
		return strings.Compare(a.Key, b.Key)
	})
	t.now = now
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make([]Entry, 0, len(t.entries))
	for _, e := range t.entries {
		entries = append(entries, e)
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		return strings.Compare(a.Key, b.Key)
	})
	return Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRevision,
		Now:          t.now,
		Entries:      entries,
	}
}
