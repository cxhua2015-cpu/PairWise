package expirytable284

import (
	"errors"
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
//
// State is kept as a hash index (entries) plus an insertion-ordered key
// list (order) so snapshots are deterministic. All mutations run as
// candidate transactions: Apply clones the state, applies expiry and ops
// on the candidate, and only commits on success, so any error rolls back
// expirations, time, and revisions together.
type Table struct {
	mu           sync.Mutex
	maxEntries   int
	maxKeyBytes  int
	generation   uint64
	nextRevision uint64
	now          int64
	entries      map[string]Entry
	order        []string
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

// state is a mutable candidate view used by transactional Apply.
type state struct {
	generation   uint64
	nextRevision uint64
	now          int64
	entries      map[string]Entry
	order        []string
}

func (t *Table) candidate() *state {
	s := &state{
		generation:   t.generation,
		nextRevision: t.nextRevision,
		now:          t.now,
		entries:      make(map[string]Entry, len(t.entries)),
		order:        make([]string, len(t.order)),
	}
	for k, v := range t.entries {
		s.entries[k] = v
	}
	copy(s.order, t.order)
	return s
}

func (s *state) expireLocked(now int64) {
	kept := s.order[:0]
	for _, k := range s.order {
		if s.entries[k].ExpiresAt <= now {
			delete(s.entries, k)
			continue
		}
		kept = append(kept, k)
	}
	s.order = kept
}

func (s *state) put(key string, expiresAt int64) {
	if _, ok := s.entries[key]; !ok {
		s.order = append(s.order, key)
	}
	s.entries[key] = Entry{Key: key, ExpiresAt: expiresAt, Revision: s.nextRevision}
	s.nextRevision++
}

func (s *state) touch(key string, expiresAt int64) error {
	e, ok := s.entries[key]
	if !ok {
		return ErrNotFound
	}
	e.ExpiresAt = expiresAt
	e.Revision = s.nextRevision
	s.nextRevision++
	s.entries[key] = e
	return nil
}

func (s *state) del(key string) error {
	if _, ok := s.entries[key]; !ok {
		return ErrNotFound
	}
	delete(s.entries, key)
	for i, k := range s.order {
		if k == key {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	if err := t.validateBatch(b); err != nil {
		return Result{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b.Now < t.now {
		return Result{}, ErrTime
	}
	s := t.candidate()
	s.now = b.Now
	s.expireLocked(b.Now)
	for _, op := range b.Ops {
		var err error
		switch op.Kind {
		case Put:
			s.put(op.Key, op.ExpiresAt)
		case Touch:
			err = s.touch(op.Key, op.ExpiresAt)
		case Delete:
			err = s.del(op.Key)
		}
		if err != nil {
			return Result{}, err
		}
	}
	if len(s.entries) > t.maxEntries {
		return Result{}, ErrCapacity
	}
	if len(b.Ops) > 0 {
		s.generation++
	}
	t.generation, t.nextRevision, t.now = s.generation, s.nextRevision, s.now
	t.entries, t.order = s.entries, s.order
	return Result{Generation: t.generation, Revision: t.nextRevision - 1}, nil
}

func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < t.now {
		return nil, ErrTime
	}
	var gone []Entry
	kept := t.order[:0]
	for _, k := range t.order {
		e := t.entries[k]
		if e.ExpiresAt <= now {
			delete(t.entries, k)
			gone = append(gone, e)
			continue
		}
		kept = append(kept, k)
	}
	t.order = kept
	t.now = now
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRevision,
		Now:          t.now,
		Entries:      make([]Entry, 0, len(t.order)),
	}
	for _, k := range t.order {
		s.Entries = append(s.Entries, t.entries[k])
	}
	return s
}
