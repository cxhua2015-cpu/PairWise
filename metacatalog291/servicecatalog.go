package metacatalog291

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
// Index: records live in a hash map keyed by name (O(1) point access);
// sorted views are materialized on demand. Logical clocks (generation,
// nextRevision) travel with the map under a single RWMutex.
type Store struct {
	mu           sync.RWMutex
	opts         Options
	records      map[string]Record
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRevision: 1}, nil
}

// Apply executes the batch atomically in input order. Structural validation
// completes before any state is read; record-count and total-value-bytes
// capacity are only checked against the final candidate state. Any failure
// rolls back records, generation and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Candidate transaction: mutate a private copy so failure is a no-op.
	candidate := make(map[string]Record, len(s.records))
	for name, rec := range s.records {
		candidate[name] = rec
	}
	nextRev := s.nextRevision
	touched := make(map[string]struct{}, len(b.Ops))

	for _, op := range b.Ops {
		touched[op.Name] = struct{}{}
		switch op.Kind {
		case Put:
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			candidate[op.Name] = Record{Name: op.Name, Value: value, Revision: nextRev}
			nextRev++
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
	}

	total := 0
	for _, rec := range candidate {
		total += len(rec.Value)
	}
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	res := Result{Generation: s.generation, Revision: nextRev - 1}
	if len(b.Ops) > 0 {
		s.records = candidate
		s.generation++
		s.nextRevision = nextRev
		res.Generation = s.generation
		names := make([]string, 0, len(touched))
		for name := range touched {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if rec, ok := candidate[name]; ok {
				res.Changed = append(res.Changed, cloneRecord(rec))
			}
		}
	}
	return res, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := validateName(name, s.opts); err != nil {
		return Record{}, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return cloneRecord(rec), true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

// snapshotLocked requires s.mu held (read or write).
func (s *Store) snapshotLocked() Snapshot {
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	snap := Snapshot{Generation: s.generation, NextRevision: s.nextRevision}
	for _, name := range names {
		snap.Records = append(snap.Records, cloneRecord(s.records[name]))
	}
	return snap
}

func cloneRecord(r Record) Record {
	value := make([]byte, len(r.Value))
	copy(value, r.Value)
	return Record{Name: r.Name, Value: value, Revision: r.Revision}
}
