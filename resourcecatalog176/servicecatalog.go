package resourcecatalog176

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

type record struct {
	value    []byte
	revision uint64
}

type Store struct {
	mu           sync.RWMutex
	records      map[string]record
	generation   uint64
	nextRevision uint64
	opts         Options
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{records: make(map[string]record), nextRevision: 1, opts: o}, nil
}

func validName(name string, maxBytes int) bool {
	if len(name) == 0 || len(name) > maxBytes {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s *Store) validate(op Op) error {
	switch op.Kind {
	case Put:
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		if len(op.Value) > s.opts.MaxValueBytes {
			return ErrInvalidInput
		}
	case Delete:
		if !validName(op.Name, s.opts.MaxNameBytes) {
			return ErrInvalidInput
		}
		if op.Value != nil {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func (s *Store) Apply(b Batch) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 1: full structural validation before any state read.
	for _, op := range b.Ops {
		if err := s.validate(op); err != nil {
			return Result{}, err
		}
	}

	// Phase 2: apply onto a candidate copy; commit only on success.
	candidate := make(map[string]record, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	nextRev := s.nextRevision
	touched := make(map[string]struct{}, len(b.Ops))
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			candidate[op.Name] = record{value: v, revision: nextRev}
			nextRev++
			touched[op.Name] = struct{}{}
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			touched[op.Name] = struct{}{}
		}
	}

	// Phase 3: capacity checks at batch end.
	if len(candidate) > s.opts.MaxRecords {
		return Result{}, ErrCapacity
	}
	total := 0
	for _, r := range candidate {
		total += len(r.value)
	}
	if total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	s.records = candidate
	s.nextRevision = nextRev
	if len(b.Ops) > 0 {
		s.generation++
	}

	names := make([]string, 0, len(touched))
	for n := range touched {
		if _, ok := s.records[n]; ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	changed := make([]Record, 0, len(names))
	for _, n := range names {
		r := s.records[n]
		changed = append(changed, Record{Name: n, Value: append([]byte(nil), r.value...), Revision: r.revision})
	}
	return Result{Generation: s.generation, Revision: s.nextRevision - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.opts.MaxNameBytes) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	return Record{Name: name, Value: append([]byte(nil), r.value...), Revision: r.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.records))
	for n := range s.records {
		names = append(names, n)
	}
	sort.Strings(names)
	recs := make([]Record, 0, len(names))
	for _, n := range names {
		r := s.records[n]
		recs = append(recs, Record{Name: n, Value: append([]byte(nil), r.value...), Revision: r.revision})
	}
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}
