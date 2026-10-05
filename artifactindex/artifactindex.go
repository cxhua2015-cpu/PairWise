// Package artifactindex implements a concurrency-safe, in-memory artifact
// index for a distributed control plane. See SPEC.md for semantics.
package artifactindex

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

// Store is a concurrency-safe in-memory artifact index.
type Store struct {
	mu         sync.Mutex
	opts       Options
	records    map[string]entry
	totalBytes int
	generation uint64
	revision   uint64
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

// validateOp performs structural validation only; it must not read state.
func (s *Store) validateOp(op Op) error {
	switch op.Kind {
	case Put:
		if !validName(op.Name, s.opts.MaxNameBytes) || len(op.Value) > s.opts.MaxValueBytes {
			return ErrInvalidInput
		}
	case Delete:
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

// Apply atomically applies the batch's ops in input order. Put allocates a
// contiguous revision per op; Delete allocates none. Structural validation of
// the whole batch happens before any state is read; record-count and total
// value-byte capacities are checked only at the end of the batch. Any failure
// rolls back all state, generation and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 1: full structural validation before reading any state.
	for _, op := range b.Ops {
		if err := s.validateOp(op); err != nil {
			return Result{}, err
		}
	}

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.revision}, nil
	}

	// Phase 2: apply ops on a candidate transaction.
	type backup struct {
		e       entry
		existed bool
	}
	backups := make(map[string]backup)
	touched := make(map[string]struct{})
	total := s.totalBytes
	savedRevision := s.revision
	revision := s.revision

	restore := func() {
		for name, bk := range backups {
			if bk.existed {
				s.records[name] = bk.e
			} else {
				delete(s.records, name)
			}
		}
		s.totalBytes = total
		s.revision = savedRevision
	}

	for _, op := range b.Ops {
		touched[op.Name] = struct{}{}
		if _, ok := backups[op.Name]; !ok {
			e, existed := s.records[op.Name]
			backups[op.Name] = backup{e: e, existed: existed}
		}
		switch op.Kind {
		case Put:
			revision++
			old, existed := s.records[op.Name]
			if existed {
				total -= len(old.value)
			}
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			s.records[op.Name] = entry{value: v, revision: revision}
			total += len(v)
		case Delete:
			old, existed := s.records[op.Name]
			if !existed {
				restore()
				return Result{}, ErrNotFound
			}
			total -= len(old.value)
			delete(s.records, op.Name)
		}
	}

	// Phase 3: capacity checks only at batch end.
	if len(s.records) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		restore()
		return Result{}, ErrCapacity
	}

	s.totalBytes = total
	s.revision = revision
	s.generation++

	names := make([]string, 0, len(touched))
	for name := range touched {
		names = append(names, name)
	}
	sort.Strings(names)
	res := Result{Generation: s.generation, Revision: s.revision}
	for _, name := range names {
		if e, ok := s.records[name]; ok {
			res.Changed = append(res.Changed, recordOf(name, e))
		}
	}
	return res, nil
}

func recordOf(name string, e entry) Record {
	v := make([]byte, len(e.value))
	copy(v, e.value)
	return Record{Name: name, Value: v, Revision: e.revision}
}

// Get returns a deep copy of the record for name.
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
	return recordOf(name, e), true, nil
}

// Snapshot returns a deep copy of the whole index, records sorted by name.
func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	snap := Snapshot{Generation: s.generation, NextRevision: s.revision + 1}
	for _, name := range names {
		snap.Records = append(snap.Records, recordOf(name, s.records[name]))
	}
	return snap
}
