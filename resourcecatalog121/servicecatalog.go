package resourcecatalog121

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
	mu           sync.RWMutex
	records      map[string]Record
	generation   uint64
	nextRevision uint64
	maxRecords   int
	maxName      int
	maxValue     int
	maxTotal     int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{
		records:      make(map[string]Record),
		nextRevision: 1,
		maxRecords:   o.MaxRecords,
		maxName:      o.MaxNameBytes,
		maxValue:     o.MaxValueBytes,
		maxTotal:     o.MaxTotalValueBytes,
	}, nil
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
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 1: full structural validation before reading any state.
	for _, op := range b.Ops {
		if op.Kind != Put && op.Kind != Delete {
			return Result{}, ErrInvalidInput
		}
		if !validName(op.Name, s.maxName) {
			return Result{}, ErrInvalidInput
		}
		if op.Kind == Put && len(op.Value) > s.maxValue {
			return Result{}, ErrInvalidInput
		}
	}

	// Phase 2: execute against a candidate copy; commit only on success.
	candidate := make(map[string]Record, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	nextRev := s.nextRevision
	changed := make(map[string]Record)
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			r := Record{Name: op.Name, Value: append([]byte(nil), op.Value...), Revision: nextRev}
			nextRev++
			candidate[op.Name] = r
			changed[op.Name] = r
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
			changed[op.Name] = Record{Name: op.Name}
		}
	}

	// Phase 3: final capacity checks at end of batch.
	if len(candidate) > s.maxRecords {
		return Result{}, ErrCapacity
	}
	total := 0
	for _, r := range candidate {
		total += len(r.Value)
	}
	if total > s.maxTotal {
		return Result{}, ErrCapacity
	}

	// Commit.
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
		r := changed[n]
		r.Value = append([]byte(nil), r.Value...)
		out = append(out, r)
	}
	return Result{Generation: s.generation, Revision: nextRev - 1, Changed: out}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if !validName(name, s.maxName) {
		return Record{}, false, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	r.Value = append([]byte(nil), r.Value...)
	return r, true, nil
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
		r.Value = append([]byte(nil), r.Value...)
		recs = append(recs, r)
	}
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}
