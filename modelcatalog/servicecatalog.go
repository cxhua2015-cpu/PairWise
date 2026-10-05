// Package modelcatalog implements a concurrency-safe, in-memory model
// catalog for a distributed control plane. See SPEC.md for semantics.
package modelcatalog

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

// Store is a concurrency-safe in-memory model catalog.
type Store struct {
	mu           sync.Mutex
	opts         Options
	records      map[string]entry
	generation   uint64
	lastRevision uint64
}

// New validates opts and returns an empty Store.
func New(opts Options) (*Store, error) {
	if opts.MaxRecords <= 0 || opts.MaxNameBytes <= 0 || opts.MaxValueBytes <= 0 || opts.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: opts, records: make(map[string]entry)}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// Apply executes batch atomically in input order and returns the result.
// The batch is fully validated structurally before any state is read;
// record-count and total-value-byte capacity are checked only at the end.
// Any failure rolls back all state, generation and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation, no state reads.
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Put && len(op.Value) > s.opts.MaxValueBytes {
			return Result{}, ErrInvalidInput
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 2: execute against a candidate copy so failure is rollback-free.
	candidate := make(map[string]entry, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		candidate[k] = v
	}
	revision := s.lastRevision
	touched := make(map[string]bool, len(b.Ops))
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			revision++
			candidate[op.Name] = entry{value: append([]byte(nil), op.Value...), revision: revision}
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
		}
		touched[op.Name] = true
	}

	// Phase 3: final capacity checks on the candidate end state.
	if len(candidate) > s.opts.MaxRecords {
		return Result{}, ErrCapacity
	}
	total := 0
	for _, e := range candidate {
		total += len(e.value)
		if total > s.opts.MaxTotalValueBytes {
			return Result{}, ErrCapacity
		}
	}

	// Commit.
	s.records = candidate
	s.lastRevision = revision
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
		if e, ok := candidate[name]; ok {
			changed = append(changed, Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision})
		} else {
			changed = append(changed, Record{Name: name})
		}
	}
	return Result{Generation: s.generation, Revision: revision, Changed: changed}, nil
}

// Get returns a deep copy of the named record.
func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision}, true, nil
}

// Snapshot returns a deep copy of the whole catalog, records sorted by name.
func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	records := make([]Record, 0, len(names))
	for _, name := range names {
		e := s.records[name]
		records = append(records, Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision})
	}
	return Snapshot{Generation: s.generation, NextRevision: s.lastRevision + 1, Records: records}
}
