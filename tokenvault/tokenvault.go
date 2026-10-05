package tokenvault

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

func (t *Table) Apply(b Batch) (Result, error) {
	for _, op := range b.Ops {
		if !validKind(op.Kind) || !validKey(op.Key, t.maxKeyBytes) {
			return Result{}, ErrInvalidInput
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b.Now < 0 || b.Now < t.now {
		return Result{}, ErrTime
	}
	if len(b.Ops) == 0 {
		t.now = b.Now
		return Result{Generation: t.generation, Revision: t.nextRevision - 1}, nil
	}
	cand := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt <= b.Now {
			continue
		}
		cand[k] = e
	}
	nextRev := t.nextRevision
	var lastRev uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			cand[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: nextRev}
			lastRev = nextRev
			nextRev++
		case Touch:
			e, ok := cand[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = nextRev
			cand[op.Key] = e
			lastRev = nextRev
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
	t.generation++
	t.nextRevision = nextRev
	return Result{Generation: t.generation, Revision: lastRev}, nil
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
