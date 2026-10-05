package resourcecatalog136

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
	mu           sync.Mutex
	opts         Options
	records      map[string]entry
	totalValue   int
	generation   uint64
	nextRevision uint64
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]entry), nextRevision: 1}, nil
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

func (s *Store) Apply(b Batch) (Result, error) {
	// Full structural validation before any state is read.
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

	// Candidate transaction: clone the index shallowly; stored values are
	// never mutated in place, so the clone is safe to discard on failure.
	candidate := make(map[string]entry, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	total := s.totalValue
	nextRev := s.nextRevision

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			value := make([]byte, len(op.Value))
			copy(value, op.Value)
			if old, ok := candidate[op.Name]; ok {
				total -= len(old.value)
			}
			candidate[op.Name] = entry{value: value, revision: nextRev}
			total += len(value)
			nextRev++
		case Delete:
			old, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			total -= len(old.value)
		}
	}

	// Capacity limits are checked only at the end of the batch.
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = candidate
	s.totalValue = total
	s.nextRevision = nextRev
	if len(b.Ops) > 0 {
		s.generation++
	}

	names := make([]string, 0, len(candidate))
	changed := make(map[string]struct{})
	for _, op := range b.Ops {
		if _, ok := changed[op.Name]; !ok {
			changed[op.Name] = struct{}{}
			names = append(names, op.Name)
		}
	}
	sort.Strings(names)
	result := Result{Generation: s.generation, Revision: s.nextRevision - 1}
	for _, name := range names {
		e := candidate[name]
		result.Changed = append(result.Changed, Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision})
	}
	return result, nil
}

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

func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	snap := Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: make([]Record, 0, len(names))}
	for _, name := range names {
		e := s.records[name]
		snap.Records = append(snap.Records, Record{Name: name, Value: append([]byte(nil), e.value...), Revision: e.revision})
	}
	return snap
}
