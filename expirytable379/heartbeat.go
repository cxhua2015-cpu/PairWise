package expirytable379

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

func validKey(k string, maxKey int) bool {
	if len(k) == 0 || len(k) > maxKey {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if !('a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{max: o.MaxEntries, maxKey: o.MaxKeyBytes, nextRev: 1, entries: map[string]Entry{}}, nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	// 1. Full structural validation before touching state.
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if !validKey(op.Key, t.maxKey) || op.ExpiresAt < 0 {
				return Result{}, ErrInvalidInput
			}
		case Delete:
			if !validKey(op.Key, t.maxKey) {
				return Result{}, ErrInvalidInput
			}
		default:
			return Result{}, ErrInvalidInput
		}
	}
	// 2. Time check: explicit, non-negative, monotonic.
	if b.Now < 0 {
		return Result{}, ErrTime
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b.Now < t.now {
		return Result{}, ErrTime
	}

	// 3. Candidate state: sweep ExpiresAt <= Now, then apply ops in order.
	cand := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt <= b.Now {
			continue
		}
		cand[k] = e
	}
	nextRev := t.nextRev
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			cand[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: nextRev}
			nextRev++
		case Touch:
			e, ok := cand[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = nextRev
			nextRev++
			cand[op.Key] = e
		case Delete:
			if _, ok := cand[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Key)
		}
	}
	// 4. Final capacity check; any failure rolls back everything.
	if len(cand) > t.max {
		return Result{}, ErrCapacity
	}

	// 5. Commit.
	t.entries = cand
	t.now = b.Now
	t.nextRev = nextRev
	if len(b.Ops) > 0 {
		t.gen++
	}
	return Result{Generation: t.gen, Revision: nextRev - 1}, nil
}

func (t *Table) Expire(now int64) ([]Entry, error) {
	if now < 0 {
		return nil, ErrTime
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
	s := Snapshot{Generation: t.gen, NextRevision: t.nextRev, Now: t.now, Entries: make([]Entry, 0, len(t.entries))}
	for _, e := range t.entries {
		s.Entries = append(s.Entries, e)
	}
	sort.Slice(s.Entries, func(i, j int) bool { return s.Entries[i].Key < s.Entries[j].Key })
	return s
}
