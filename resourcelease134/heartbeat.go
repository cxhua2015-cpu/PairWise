package resourcelease134

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

type Table struct {
	mu      sync.Mutex
	maxEnt  int
	maxKey  int
	now     int64
	gen     uint64
	rev     uint64
	entries []Entry
	index   map[string]int
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{maxEnt: o.MaxEntries, maxKey: o.MaxKeyBytes, index: map[string]int{}}, nil
}

func validKey(k string, max int) bool {
	if len(k) == 0 || len(k) > max {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func (t *Table) validate(b Batch) error {
	if b.Now < 0 {
		return ErrInvalidInput
	}
	for _, op := range b.Ops {
		switch op.Kind {
		case Put, Touch:
			if op.ExpiresAt < 0 {
				return ErrInvalidInput
			}
		case Delete:
		default:
			return ErrInvalidInput
		}
		if !validKey(op.Key, t.maxKey) {
			return ErrInvalidInput
		}
	}
	return nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.validate(b); err != nil {
		return Result{}, err
	}
	if b.Now < t.now {
		return Result{}, ErrTime
	}
	// Candidate state: work on copies so any failure rolls back
	// expirations, time and revisions together.
	entries := make([]Entry, len(t.entries))
	copy(entries, t.entries)
	index := make(map[string]int, len(t.index))
	for k, v := range t.index {
		index[k] = v
	}
	rev := t.rev
	// Expire closed-boundary entries first.
	kept := entries[:0]
	for _, e := range entries {
		if e.ExpiresAt <= b.Now {
			delete(index, e.Key)
			continue
		}
		index[e.Key] = len(kept)
		kept = append(kept, e)
	}
	entries = kept
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			rev++
			if i, ok := index[op.Key]; ok {
				entries[i] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: rev}
			} else {
				index[op.Key] = len(entries)
				entries = append(entries, Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: rev})
			}
		case Touch:
			i, ok := index[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			rev++
			entries[i] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: rev}
		case Delete:
			i, ok := index[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			entries = append(entries[:i], entries[i+1:]...)
			delete(index, op.Key)
			for j := i; j < len(entries); j++ {
				index[entries[j].Key] = j
			}
		}
	}
	if len(entries) > t.maxEnt {
		return Result{}, ErrCapacity
	}
	t.entries = entries
	t.index = index
	t.now = b.Now
	if len(b.Ops) > 0 {
		t.gen++
	}
	t.rev = rev
	return Result{Generation: t.gen, Revision: rev}, nil
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
	kept := t.entries[:0]
	for _, e := range t.entries {
		if e.ExpiresAt <= now {
			gone = append(gone, e)
			delete(t.index, e.Key)
			continue
		}
		t.index[e.Key] = len(kept)
		kept = append(kept, e)
	}
	t.entries = kept
	t.now = now
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make([]Entry, len(t.entries))
	copy(entries, t.entries)
	return Snapshot{Generation: t.gen, NextRevision: t.rev + 1, Now: t.now, Entries: entries}
}
