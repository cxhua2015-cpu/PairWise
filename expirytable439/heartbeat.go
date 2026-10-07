package expirytable439

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
type Table struct {
	mu      sync.RWMutex
	opts    Options
	now     int64
	gen     uint64
	nextRev uint64
	index   map[string]int
	entries []Entry
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{opts: o, nextRev: 1, index: make(map[string]int)}, nil
}

func (t *Table) Apply(b Batch) (Result, error) {
	if err := t.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b.Now < 0 || b.Now < t.now {
		return Result{}, ErrTime
	}

	// Candidate state: evict ExpiresAt <= Now, then apply ops in order.
	idx := make(map[string]int, len(t.index))
	for k, v := range t.index {
		idx[k] = v
	}
	entries := make([]Entry, len(t.entries))
	copy(entries, t.entries)
	nextRev := t.nextRev

	kept := entries[:0]
	for _, e := range entries {
		if e.ExpiresAt <= b.Now {
			delete(idx, e.Key)
			continue
		}
		idx[e.Key] = len(kept)
		kept = append(kept, e)
	}
	entries = kept

	for _, op := range b.Ops {
		i, ok := idx[op.Key]
		switch op.Kind {
		case Put:
			e := Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: nextRev}
			nextRev++
			if ok {
				entries[i] = e
			} else {
				idx[op.Key] = len(entries)
				entries = append(entries, e)
			}
		case Touch:
			if !ok {
				return Result{}, ErrNotFound
			}
			entries[i].ExpiresAt = op.ExpiresAt
			entries[i].Revision = nextRev
			nextRev++
		case Delete:
			if !ok {
				return Result{}, ErrNotFound
			}
			last := len(entries) - 1
			entries[i] = entries[last]
			idx[entries[i].Key] = i
			entries = entries[:last]
			delete(idx, op.Key)
		}
	}
	if len(entries) > t.opts.MaxEntries {
		return Result{}, ErrCapacity
	}

	t.now = b.Now
	t.entries = entries
	t.index = idx
	t.nextRev = nextRev
	if len(b.Ops) > 0 {
		t.gen++
	}
	return Result{Generation: t.gen, Revision: nextRev - 1}, nil
}

func (t *Table) Expire(now int64) ([]Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now < 0 || now < t.now {
		return nil, ErrTime
	}
	t.now = now
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
	for i := len(kept); i < len(t.entries); i++ {
		t.entries[i] = Entry{}
	}
	t.entries = kept
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	entries := make([]Entry, len(t.entries))
	copy(entries, t.entries)
	return Snapshot{
		Generation:   t.gen,
		NextRevision: t.nextRev,
		Now:          t.now,
		Entries:      entries,
	}
}
