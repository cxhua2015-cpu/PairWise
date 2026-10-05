package resourcecatalog196

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

type Store struct {
	mu          sync.Mutex
	opts        Options
	records     map[string]Record
	totalValues int
	generation  uint64
	nextRev     uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]Record), nextRev: 1}, nil
}

func validName(name string, maxBytes int) bool {
	if name == "" || len(name) > maxBytes {
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

func (s *Store) Apply(b Batch) (Result, error) {
	// Phase 1: full structural validation before any state read.
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return Result{}, ErrInvalidInput
		}
		if len(op.Value) > s.opts.MaxValueBytes {
			return Result{}, ErrInvalidInput
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(b.Ops) == 0 {
		return Result{Generation: s.generation, Revision: s.nextRev - 1}, nil
	}

	// Phase 2: execute against a candidate view; commit only on success.
	candidate := make(map[string]Record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		candidate[k] = v
	}
	total := s.totalValues
	rev := s.nextRev
	changed := make(map[string]Record)

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			if old, ok := candidate[op.Name]; ok {
				total -= len(old.Value)
			}
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			rec := Record{Name: op.Name, Value: value, Revision: rev}
			rev++
			candidate[op.Name] = rec
			total += len(value)
			changed[op.Name] = rec
		case Delete:
			old, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			total -= len(old.Value)
			changed[op.Name] = Record{Name: op.Name}
		}
	}

	// Phase 3: final capacity checks at end of batch.
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = candidate
	s.totalValues = total
	s.generation++
	s.nextRev = rev

	names := make([]string, 0, len(changed))
	for name := range changed {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Record, 0, len(names))
	for _, name := range names {
		rec := changed[name]
		if rec.Value != nil {
			cp := make([]byte, len(rec.Value))
			copy(cp, rec.Value)
			rec.Value = cp
		}
		out = append(out, rec)
	}
	return Result{Generation: s.generation, Revision: rev - 1, Changed: out}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	value := make([]byte, len(rec.Value))
	copy(value, rec.Value)
	rec.Value = value
	return rec, true, nil
}

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
		rec := s.records[name]
		value := make([]byte, len(rec.Value))
		copy(value, rec.Value)
		rec.Value = value
		records = append(records, rec)
	}
	return Snapshot{Generation: s.generation, NextRevision: s.nextRev, Records: records}
}
