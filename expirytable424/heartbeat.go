package expirytable424

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

type entry struct {
	expiresAt int64
	revision  uint64
}

type state struct {
	generation   uint64
	nextRevision uint64
	now          int64
	entries      map[string]entry
}

type Table struct {
	mu  sync.Mutex
	opt Options
	st  state
}

// New creates a table with positive capacity and key-length limits.
func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	t := &Table{opt: o}
	t.st.entries = make(map[string]entry)
	t.st.nextRevision = 1
	return t, nil
}

// Apply validates the batch structurally, enforces monotonic time, sweeps expired
// entries (ExpiresAt <= Now), then executes the ops in order. Any failure rolls
// every change, including evictions, logical time and allocated revisions, back.
func (t *Table) Apply(b Batch) (Result, error) {
	if err := ValidateBatch(t.opt, b); err != nil {
		return Result{}, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if b.Now < t.st.now {
		return Result{}, ErrTime
	}

	cand, res, err := t.st.commitCandidate(t.opt, b)
	if err != nil {
		return Result{}, err
	}
	t.st = cand
	return res, nil
}

// Expire removes every entry whose ExpiresAt <= now and returns the removed
// entries sorted by key. Logical time never moves backwards.
func (t *Table) Expire(now int64) ([]Entry, error) {
	if now < 0 {
		return nil, ErrTime
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if now < t.st.now {
		return nil, ErrTime
	}
	t.st.now = now

	removed := make([]Entry, 0)
	for k, v := range t.st.entries {
		if v.expiresAt <= now {
			removed = append(removed, Entry{Key: k, ExpiresAt: v.expiresAt, Revision: v.revision})
			delete(t.st.entries, k)
		}
	}
	sortEntries(removed)
	return removed, nil
}

// Snapshot returns an isolated, key-sorted view of the table.
func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.st.snapshot()
}

// commitCandidate runs the full transaction on a copied state. The receiver is
// never mutated, so a returned error is a complete rollback for the caller.
func (s state) commitCandidate(opt Options, b Batch) (state, Result, error) {
	cand := s.cloneState()
	cand.now = b.Now

	for k, v := range cand.entries {
		if v.expiresAt <= b.Now {
			delete(cand.entries, k)
		}
	}

	var lastRevision uint64
	mutated := false
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if _, exists := cand.entries[op.Key]; !exists {
				if len(cand.entries) >= opt.MaxEntries {
					return state{}, Result{}, ErrCapacity
				}
			}
			rev := cand.nextRevision
			cand.nextRevision++
			cand.entries[op.Key] = entry{expiresAt: op.ExpiresAt, revision: rev}
			lastRevision = rev
			mutated = true
		case Touch:
			v, exists := cand.entries[op.Key]
			if !exists {
				return state{}, Result{}, ErrNotFound
			}
			rev := cand.nextRevision
			cand.nextRevision++
			v.expiresAt = op.ExpiresAt
			v.revision = rev
			cand.entries[op.Key] = v
			lastRevision = rev
			mutated = true
		case Delete:
			if _, exists := cand.entries[op.Key]; exists {
				delete(cand.entries, op.Key)
				mutated = true
			}
		}
	}

	if len(b.Ops) > 0 && mutated {
		cand.generation++
	}
	return cand, Result{Generation: cand.generation, Revision: lastRevision}, nil
}

func (s state) cloneState() state {
	entries := make(map[string]entry, len(s.entries))
	for k, v := range s.entries {
		entries[k] = v
	}
	return state{
		generation:   s.generation,
		nextRevision: s.nextRevision,
		now:          s.now,
		entries:      entries,
	}
}

func (s state) snapshot() Snapshot {
	out := make([]Entry, 0, len(s.entries))
	for k, v := range s.entries {
		out = append(out, Entry{Key: k, ExpiresAt: v.expiresAt, Revision: v.revision})
	}
	sortEntries(out)
	return Snapshot{
		Generation:   s.generation,
		NextRevision: s.nextRevision,
		Now:          s.now,
		Entries:      out,
	}
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
}
