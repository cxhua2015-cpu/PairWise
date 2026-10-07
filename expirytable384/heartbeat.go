package expirytable384

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
	mu         sync.Mutex
	maxEntries int
	maxKey     int
	now        int64
	generation uint64
	revision   uint64
	entries    map[string]Entry
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{
		maxEntries: o.MaxEntries,
		maxKey:     o.MaxKeyBytes,
		entries:    make(map[string]Entry),
	}, nil
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
	t.mu.Lock()
	defer t.mu.Unlock()

	// 1. Structural validation of the whole batch before reading state.
	if b.Now < 0 {
		return Result{}, ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if op.ExpiresAt < 0 {
				return Result{}, ErrInvalidInput
			}
		case Delete:
		default:
			return Result{}, ErrInvalidInput
		}
		if !validKey(op.Key, t.maxKey) {
			return Result{}, ErrInvalidInput
		}
	}

	// 2. Monotonic time check.
	if b.Now < t.now {
		return Result{}, ErrTime
	}

	// Empty batch: validate only, state unchanged.
	if len(b.Ops) == 0 {
		return Result{Generation: t.generation, Revision: t.revision}, nil
	}

	// 3. Candidate state: expire closed-interval entries, then run ops in order.
	cand := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt <= b.Now {
			continue
		}
		cand[k] = e
	}
	rev := t.revision
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

	// 4. Final capacity check; any failure rolls back everything.
	if len(cand) > t.maxEntries {
		return Result{}, ErrCapacity
	}

	// 5. Commit.
	t.entries = cand
	t.now = b.Now
	t.revision = rev
	t.generation++
	return Result{Generation: t.generation, Revision: rev}, nil
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
	t.now = now
	sort.Slice(gone, func(i, j int) bool { return gone[i].Key < gone[j].Key })
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Snapshot{
		Generation:   t.generation,
		NextRevision: t.revision + 1,
		Now:          t.now,
		Entries:      make([]Entry, 0, len(t.entries)),
	}
	for _, e := range t.entries {
		s.Entries = append(s.Entries, e)
	}
	sort.Slice(s.Entries, func(i, j int) bool { return s.Entries[i].Key < s.Entries[j].Key })
	return s
}
