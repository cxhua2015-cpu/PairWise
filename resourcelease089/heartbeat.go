package resourcelease089

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
	mu          sync.Mutex
	maxEntries  int
	maxKeyBytes int
	now         int64
	generation  uint64
	nextRev     uint64
	entries     map[string]Entry
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		maxEntries:  o.MaxEntries,
		maxKeyBytes: o.MaxKeyBytes,
		nextRev:     1,
		entries:     make(map[string]Entry),
	}, nil
}

func validKey(k string, maxBytes int) bool {
	if len(k) == 0 || len(k) > maxBytes {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// validate performs full structural validation of the batch before any
// state is read or mutated.
func (t *Table) validate(b *Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for i := range b.Ops {
		op := &b.Ops[i]
		switch op.Kind {
		case Put, Touch:
			if op.ExpiresAt < 0 {
				return ErrInvalidInput
			}
		case Delete:
			if op.ExpiresAt != 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
		if !validKey(op.Key, t.maxKeyBytes) {
			return ErrInvalidInput
		}
	}
	return nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	if err := t.validate(&b); err != nil {
		return Result{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b.Now < t.now {
		return Result{}, ErrTime
	}
	// Candidate state: clone entries so any failure rolls back
	// evictions, time and revisions together.
	cand := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt <= b.Now {
			continue
		}
		cand[k] = e
	}
	rev := t.nextRev
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			rev++
			cand[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: rev - 1}
		case Touch:
			_, ok := cand[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			rev++
			cand[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: rev - 1}
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
	t.nextRev = rev
	if len(b.Ops) > 0 {
		t.generation++
	}
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
			gone = append(gone, e)
			delete(t.entries, k)
		}
	}
	t.now = now
	sort.Slice(gone, func(i, j int) bool { return gone[i].Key < gone[j].Key })
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRev,
		Now:          t.now,
	}
	for _, e := range t.entries {
		s.Entries = append(s.Entries, e)
	}
	sort.Slice(s.Entries, func(i, j int) bool { return s.Entries[i].Key < s.Entries[j].Key })
	return s
}
