package metacatalog276

import (
	"errors"
	"sort"
	"sync"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrInvalidOptions = errors.New("invalid options")
	ErrInvalidInput   = errors.New("invalid input")
	ErrNotFound       = errors.New("not found")
	ErrCapacity       = errors.New("capacity exceeded")
)

type Kind uint8

const (
	Put Kind = iota + 1
	Delete
)

type Options struct{ MaxRecords, MaxNameBytes, MaxValueBytes, MaxTotalValueBytes int }
type Op struct {
	Kind  Kind
	Name  string
	Value []byte
}
type Batch struct{ Ops []Op }
type Record struct {
	Name     string
	Value    []byte
	Revision uint64
}
type Result struct {
	Generation, Revision uint64
	Changed              []Record
}
type Snapshot struct {
	Generation, NextRevision uint64
	Records                  []Record
}

type entry struct {
	value    []byte
	revision uint64
}

type Store struct {
	mu           sync.RWMutex
	opts         Options
	records      map[string]entry
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]entry), nextRevision: 1}, nil
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// Apply executes the batch atomically in input order. Put allocates a
// consecutive revision; Delete does not. Structural validation happens
// before any state read; record-count and total-value-byte capacity are
// checked only against the final state. Any failure rolls back all state,
// generation and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	candidate := make(map[string]entry, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	changed := make(map[string]Record)
	nextRev := s.nextRevision
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			e := entry{value: cloneBytes(op.Value), revision: nextRev}
			candidate[op.Name] = e
			changed[op.Name] = Record{Name: op.Name, Value: cloneBytes(e.value), Revision: e.revision}
			nextRev++
		case Delete:
			old, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			changed[op.Name] = Record{Name: op.Name, Value: cloneBytes(old.value), Revision: old.revision}
		}
	}

	total := 0
	for _, e := range candidate {
		total += len(e.value)
	}
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = candidate
	s.nextRevision = nextRev
	if len(b.Ops) > 0 {
		s.generation++
	}
	names := make([]string, 0, len(changed))
	for n := range changed {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Record, 0, len(names))
	for _, n := range names {
		out = append(out, changed[n])
	}
	return Result{Generation: s.generation, Revision: nextRev - 1, Changed: out}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := validateName(name, s.opts.MaxNameBytes); err != nil {
		return Record{}, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{Name: name, Value: cloneBytes(e.value), Revision: e.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() Snapshot {
	names := make([]string, 0, len(s.records))
	for n := range s.records {
		names = append(names, n)
	}
	sort.Strings(names)
	recs := make([]Record, 0, len(names))
	for _, n := range names {
		e := s.records[n]
		recs = append(recs, Record{Name: n, Value: cloneBytes(e.value), Revision: e.revision})
	}
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}
