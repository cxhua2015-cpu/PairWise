package resourcelease104

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

func validKind(k Kind) bool { return k == Put || k == Touch || k == Delete }

func sortedEntries(m map[string]Entry) []Entry {
	out := make([]Entry, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (t *Table) Apply(b Batch) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 1. Structural validation of the whole batch before touching state.
	for _, op := range b.Ops {
		if !validKind(op.Kind) || !validKey(op.Key, t.maxKeyBytes) {
			return Result{}, ErrInvalidInput
		}
	}
	// 2. Time check: explicit, non-negative, monotonic.
	if b.Now < 0 || b.Now < t.now {
		return Result{}, ErrTime
	}
	// Empty batches change nothing.
	if len(b.Ops) == 0 {
		return Result{Generation: t.generation, Revision: t.nextRev - 1}, nil
	}

	// 3. Candidate state: evict ExpiresAt <= Now, then apply ops in order.
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
			if _, ok := cand[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			cand[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: nextRev}
			nextRev++
		case Delete:
			if _, ok := cand[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(cand, op.Key)
		}
	}
	// 4. Final capacity check; any failure discards the candidate wholesale.
	if len(cand) > t.maxEntries {
		return Result{}, ErrCapacity
	}

	// 5. Commit.
	t.entries = cand
	t.now = b.Now
	t.nextRev = nextRev
	t.generation++
	return Result{Generation: t.generation, Revision: nextRev - 1}, nil
}

func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < 0 || now < t.now {
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
	if len(gone) > 0 {
		t.generation++
	}
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRev,
		Now:          t.now,
		Entries:      sortedEntries(t.entries),
	}
}
