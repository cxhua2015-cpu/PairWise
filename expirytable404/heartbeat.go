package expirytable404

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
type entry struct {
	expiresAt int64
	revision  uint64
}

type Table struct {
	mu          sync.RWMutex
	maxEntries  int
	maxKeyBytes int
	now         int64
	generation  uint64
	nextRev     uint64
	entries     map[string]entry
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		maxEntries:  o.MaxEntries,
		maxKeyBytes: o.MaxKeyBytes,
		nextRev:     1,
		entries:     make(map[string]entry),
	}, nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	if err := t.validate(b); err != nil {
		return Result{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b.Now < t.now {
		return Result{}, ErrTime
	}
	// Candidate state: clone-on-write so any failure rolls back
	// expirations, time and revisions together.
	cand := make(map[string]entry, len(t.entries))
	for k, v := range t.entries {
		if v.expiresAt > b.Now {
			cand[k] = v
		}
	}
	nextRev := t.nextRev
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			cand[op.Key] = entry{expiresAt: op.ExpiresAt, revision: nextRev}
			nextRev++
		case Touch:
			_, ok := cand[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			cand[op.Key] = entry{expiresAt: op.ExpiresAt, revision: nextRev}
			nextRev++
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
	t.nextRev = nextRev
	if len(b.Ops) > 0 {
		t.generation++
	}
	return Result{Generation: t.generation, Revision: nextRev - 1}, nil
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
	for k, v := range t.entries {
		if v.expiresAt <= now {
			gone = append(gone, Entry{Key: k, ExpiresAt: v.expiresAt, Revision: v.revision})
			delete(t.entries, k)
		}
	}
	t.now = now
	sortEntries(gone)
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s := Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRev,
		Now:          t.now,
		Entries:      make([]Entry, 0, len(t.entries)),
	}
	for k, v := range t.entries {
		s.Entries = append(s.Entries, Entry{Key: k, ExpiresAt: v.expiresAt, Revision: v.revision})
	}
	sortEntries(s.Entries)
	return s
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool { return es[i].Key < es[j].Key })
}
