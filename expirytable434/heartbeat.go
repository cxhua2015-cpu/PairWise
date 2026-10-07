package expirytable434

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

// Table is a concurrency-safe in-memory expiry state table.
type Table struct {
	mu           sync.Mutex
	opts         Options
	entries      map[string]Entry
	generation   uint64
	nextRevision uint64
	now          int64
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{opts: o, entries: make(map[string]Entry), nextRevision: 1}, nil
}

// state is a mutable candidate view used by transactional apply.
type state struct {
	entries      map[string]Entry
	generation   uint64
	nextRevision uint64
	now          int64
}

func (t *Table) snapshotState() *state {
	entries := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		entries[k] = e
	}
	return &state{entries: entries, generation: t.generation, nextRevision: t.nextRevision, now: t.now}
}

// apply executes the full transaction semantics on a candidate state:
// expire entries with ExpiresAt <= Now, then run ops in order.
func (s *state) apply(b Batch) (Result, error) {
	for k, e := range s.entries {
		if e.ExpiresAt <= b.Now {
			delete(s.entries, k)
		}
	}
	var lastRev uint64
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			s.entries[op.Key] = Entry{Key: op.Key, ExpiresAt: op.ExpiresAt, Revision: s.nextRevision}
			lastRev = s.nextRevision
			s.nextRevision++
		case Touch:
			e, ok := s.entries[op.Key]
			if !ok {
				return Result{}, ErrNotFound
			}
			e.ExpiresAt = op.ExpiresAt
			e.Revision = s.nextRevision
			s.entries[op.Key] = e
			lastRev = s.nextRevision
			s.nextRevision++
		case Delete:
			if _, ok := s.entries[op.Key]; !ok {
				return Result{}, ErrNotFound
			}
			delete(s.entries, op.Key)
		}
	}
	return Result{Revision: lastRev}, nil
}

func sortedEntries(m map[string]Entry) []Entry {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Entry, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

func (t *Table) Apply(b Batch) (Result, error) {
	if err := validateBatch(t.opts, b); err != nil {
		return Result{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b.Now < t.now {
		return Result{}, ErrTime
	}
	cand := t.snapshotState()
	res, err := cand.apply(b)
	if err != nil {
		return Result{}, err
	}
	if len(cand.entries) > t.opts.MaxEntries {
		return Result{}, ErrCapacity
	}
	if len(b.Ops) > 0 {
		cand.generation++
	}
	cand.now = b.Now
	t.entries, t.generation, t.nextRevision, t.now = cand.entries, cand.generation, cand.nextRevision, cand.now
	res.Generation = t.generation
	if res.Revision == 0 {
		res.Revision = t.nextRevision - 1
	}
	return res, nil
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
			delete(t.entries, k)
			gone = append(gone, e)
		}
	}
	t.now = now
	sort.Slice(gone, func(i, j int) bool { return gone[i].Key < gone[j].Key })
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return Snapshot{
		Generation:   t.generation,
		NextRevision: t.nextRevision,
		Now:          t.now,
		Entries:      sortedEntries(t.entries),
	}
}
