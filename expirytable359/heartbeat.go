package expirytable359

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

type Table struct {
	mu      sync.Mutex
	max     int
	maxKey  int
	now     int64
	gen     uint64
	nextRev uint64
	entries map[string]Entry
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{max: o.MaxEntries, maxKey: o.MaxKeyBytes, nextRev: 1, entries: map[string]Entry{}}, nil
}

func validKey(k string, max int) bool {
	if len(k) == 0 || len(k) > max {
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

func (t *Table) Apply(b Batch) (Result, error) {
	// 1. Full structural validation before reading state.
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Touch && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validKey(op.Key, t.maxKey) {
			return Result{}, ErrInvalidInput
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// 2. Time check: explicit, non-negative, monotonic.
	if b.Now < 0 || b.Now < t.now {
		return Result{}, ErrTime
	}

	// Empty batch: no state change, generation unchanged.
	if len(b.Ops) == 0 {
		return Result{Generation: t.gen, Revision: t.nextRev - 1}, nil
	}

	// Candidate state: clone so any failure rolls back completely.
	cand := make(map[string]Entry, len(t.entries))
	for k, v := range t.entries {
		cand[k] = v
	}
	nextRev := t.nextRev
	lastRev := t.nextRev - 1

	// Evict ExpiresAt <= Now on the candidate first.
	for k, v := range cand {
		if v.ExpiresAt <= b.Now {
			delete(cand, k)
		}
	}

	// Execute ops in order.
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			cand[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: nextRev}
			lastRev, nextRev = nextRev, nextRev+1
		case Touch:
			e, ok := cand[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = nextRev
			cand[op.Key] = e
			lastRev, nextRev = nextRev, nextRev+1
		case Delete:
			if _, ok := cand[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Key)
		}
	}

	// Final capacity check; failure rolls back evictions, time and revisions.
	if len(cand) > t.max {
		return Result{}, ErrCapacity
	}

	t.entries = cand
	t.now = b.Now
	t.nextRev = nextRev
	t.gen++
	return Result{Generation: t.gen, Revision: lastRev}, nil
}

func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < 0 || now < t.now {
		return nil, ErrTime
	}
	var gone []Entry
	for k, v := range t.entries {
		if v.ExpiresAt <= now {
			gone = append(gone, v)
			delete(t.entries, k)
		}
	}
	sort.Slice(gone, func(i, j int) bool { return gone[i].Key < gone[j].Key })
	t.now = now
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Snapshot{Generation: t.gen, NextRevision: t.nextRev, Now: t.now}
	for _, v := range t.entries {
		s.Entries = append(s.Entries, v)
	}
	sort.Slice(s.Entries, func(i, j int) bool { return s.Entries[i].Key < s.Entries[j].Key })
	return s
}
