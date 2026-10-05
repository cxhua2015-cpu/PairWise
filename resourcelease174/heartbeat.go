package resourcelease174

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

func validKey(key string, maxBytes int) bool {
	if len(key) == 0 || len(key) > maxBytes {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func validateOp(o Op, maxKeyBytes int) error {
	switch o.Kind {
	case Put, Touch, Delete:
	default:
		return ErrInvalidInput
	}
	if o.Kind != Delete && o.ExpiresAt < 0 {
		return ErrInvalidInput
	}
	if !validKey(o.Key, maxKeyBytes) {
		return ErrInvalidInput
	}
	return nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	for _, o := range b.Ops {
		if err := validateOp(o, t.maxKeyBytes); err != nil {
			return Result{}, err
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

	// Candidate state: work on a copy so any failure rolls back
	// expirations, time and revisions together.
	cand := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt > b.Now {
			cand[k] = e
		}
	}
	nextRev := t.nextRevision
	for _, o := range b.Ops {
		switch o.Kind {
		case Put:
			cand[o.Key] = Entry{Key: o.Key, ExpiresAt: o.ExpiresAt, Revision: nextRev}
			nextRev++
		case Touch:
			e, ok := cand[o.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = o.ExpiresAt
			e.Revision = nextRev
			nextRev++
			cand[o.Key] = e
		case Delete:
			if _, ok := cand[o.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, o.Key)
		}
	}
	if len(cand) > t.maxEntries {
		return Result{}, ErrCapacity
	}

	t.entries = cand
	t.now = b.Now
	t.nextRevision = nextRev
	t.generation++
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
	t.now = now
	var gone []Entry
	for k, e := range t.entries {
		if e.ExpiresAt <= now {
			gone = append(gone, e)
			delete(t.entries, k)
		}
	}
	sort.Slice(gone, func(i, j int) bool { return gone[i].Key < gone[j].Key })
	return gone, nil
}

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
