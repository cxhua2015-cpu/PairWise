package expirytable224

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
	opts    Options
	now     int64
	gen     uint64
	rev     uint64
	entries map[string]Entry
}

func New(o Options) (*Table, error) {
	if o.MaxEntries <= 0 || o.MaxKeyBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Table{opts: o, entries: make(map[string]Entry)}, nil
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
	if len(b.Ops) == 0 {
		return Result{Generation: t.gen, Revision: t.rev}, nil
	}
	cand := make(map[string]Entry, len(t.entries))
	for k, e := range t.entries {
		if e.ExpiresAt > b.Now {
			cand[k] = e
		}
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
	if len(cand) > t.opts.MaxEntries {
		return Result{}, ErrCapacity
	}
	t.entries = cand
	t.now = b.Now
	t.rev = rev
	t.gen++
	return Result{Generation: t.gen, Revision: rev}, nil
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
	t.now = now
	var gone []Entry
	for k, e := range t.entries {
		if e.ExpiresAt <= now {
			gone = append(gone, e)
			delete(t.entries, k)
		}
	}
	sortEntries(gone)
	return gone, nil
}

func (t *Table) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked()
}

func (t *Table) snapshotLocked() Snapshot {
	es := make([]Entry, 0, len(t.entries))
	for _, e := range t.entries {
		es = append(es, e)
	}
	sortEntries(es)
	return Snapshot{
		Generation:   t.gen,
		NextRevision: t.rev + 1,
		Now:          t.now,
		Entries:      es,
	}
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool { return es[i].Key < es[j].Key })
}
