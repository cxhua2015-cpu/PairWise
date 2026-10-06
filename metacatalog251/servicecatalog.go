package metacatalog251

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

// Store is a concurrency-safe in-memory metadata catalog.
//
// Internally it keeps a single index (map keyed by name) plus a logical
// clock (generation, nextRevision). Apply runs each batch as a candidate
// transaction against a private copy of the index and commits atomically
// only after the final capacity checks pass, so failures leave all state,
// generation and revision untouched.
type Store struct {
	mu              sync.RWMutex
	opts            Options
	index           map[string]Record
	generation      uint64
	nextRevision    uint64
	totalValueBytes int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, index: make(map[string]Record), nextRevision: 1}, nil
}

// Apply executes the batch atomically in input order. Put allocates
// consecutive revisions; Delete allocates none. Record-count and total
// value-byte capacities are checked only at the end of the batch; any
// failure rolls back all state, generation and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Candidate transaction: apply ops on a private copy of the index.
	candidate := make(map[string]Record, len(s.index)+len(b.Ops))
	for name, rec := range s.index {
		candidate[name] = rec
	}
	total := s.totalValueBytes
	nextRev := s.nextRevision
	touched := make(map[string]struct{}, len(b.Ops))

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			old, existed := candidate[op.Name]
			if existed {
				total -= len(old.Value)
			}
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			candidate[op.Name] = Record{Name: op.Name, Value: value, Revision: nextRev}
			nextRev++
			total += len(value)
			touched[op.Name] = struct{}{}
		case Delete:
			old, existed := candidate[op.Name]
			if !existed {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			total -= len(old.Value)
			touched[op.Name] = struct{}{}
		}
	}

	// Final capacity checks, only at batch end.
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.index = candidate
	s.totalValueBytes = total
	s.nextRevision = nextRev
	if len(b.Ops) > 0 {
		s.generation++
	}

	names := make([]string, 0, len(touched))
	for name := range touched {
		names = append(names, name)
	}
	sort.Strings(names)
	changed := make([]Record, 0, len(names))
	for _, name := range names {
		if rec, ok := candidate[name]; ok {
			changed = append(changed, cloneRecord(rec))
		}
	}

	return Result{Generation: s.generation, Revision: s.nextRevision - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.index[name]
	if !ok {
		return Record{}, false, nil
	}
	return cloneRecord(rec), true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.index))
	for name := range s.index {
		names = append(names, name)
	}
	sort.Strings(names)
	records := make([]Record, 0, len(names))
	for _, name := range names {
		records = append(records, cloneRecord(s.index[name]))
	}
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: records}
}

func cloneRecord(r Record) Record {
	value := make([]byte, len(r.Value))
	copy(value, r.Value)
	return Record{Name: r.Name, Value: value, Revision: r.Revision}
}
