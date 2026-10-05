package resourcelease149

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
	entries     map[string]Entry
	now         int64
	gen         uint64
	rev         uint64
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

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		maxEntries:  o.MaxEntries,
		maxKeyBytes: o.MaxKeyBytes,
		entries:     make(map[string]Entry),
	}, nil
}

// validate checks the whole batch structurally before any state is read.
func (t *Table) validate(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if !validKey(op.Key, t.maxKeyBytes) || op.ExpiresAt < 0 {
				return ErrInvalidInput
			}
		case Delete:
			if !validKey(op.Key, t.maxKeyBytes) {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Structural validation of the full batch happens before touching state.
	if err := t.validate(b); err != nil {
		return Result{}, err
	}
	if b.Now < t.now {
		return Result{}, ErrTime
	}
	// Candidate state: work on a copy so any failure rolls back cleanly.
	cand := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt <= b.Now {
			continue // expire entries at the closed-boundary
		}
		cand[k] = e
	}
	rev := t.rev
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			rev++
			cand[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: rev}
		case Touch:
			e, ok := cand[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			rev++
			e.ExpiresAt = op.ExpiresAt
			e.Revision = rev
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
	// Commit: swap candidate in, advance time and revision.
	t.entries = cand
	t.now = b.Now
	t.rev = rev
	if len(b.Ops) > 0 {
		t.gen++
	}
	return Result{Generation: t.gen, Revision: t.rev}, nil
}

func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < 0 {
		return nil, ErrInvalidInput
	}
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
	sort.Slice(gone, func(i, j int) bool { return gone[i].Key < gone[j].Key })
	t.now = now
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Snapshot{
		Generation:   t.gen,
		NextRevision: t.rev + 1,
		Now:          t.now,
		Entries:      make([]Entry, 0, len(t.entries)),
	}
	for _, e := range t.entries {
		s.Entries = append(s.Entries, e)
	}
	sort.Slice(s.Entries, func(i, j int) bool { return s.Entries[i].Key < s.Entries[j].Key })
	return s
}
