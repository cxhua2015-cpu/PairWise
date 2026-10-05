package resourcecatalog186

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
	mu         sync.RWMutex
	opts       Options
	records    map[string]record
	generation uint64
	revision   uint64
	totalBytes int
}

func New(o Options) (*Store, error) {
	if o.MaxRecords <= 0 || o.MaxNameBytes <= 0 || o.MaxValueBytes <= 0 || o.MaxTotalValueBytes <= 0 {
		return nil, ErrInvalidOptions
	}
	return &Store{opts: o, records: make(map[string]record)}, nil
}

func validName(name string, max int) bool {
	if len(name) == 0 || len(name) > max {
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

	// Candidate transaction: apply ops in input order on a clone.
	candidate := make(map[string]record, len(s.records)+len(b.Ops))
	for k, v := range s.records {
		candidate[k] = v
	}
	totalBytes := s.totalBytes
	revision := s.revision
	touched := make(map[string]struct{}, len(b.Ops))

	for _, op := range b.Ops {
		touched[op.Name] = struct{}{}
		switch op.Kind {
		case Put:
			if old, ok := candidate[op.Name]; ok {
				totalBytes -= len(old.value)
			}
			revision++
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			candidate[op.Name] = record{value: v, revision: revision}
			totalBytes += len(v)
		case Delete:
			old, ok := candidate[op.Name]
			if !ok {
				return Result{}, ErrNotFound
			}
			totalBytes -= len(old.value)
			delete(candidate, op.Name)
		}
	}

	// Capacity limits are only checked at the end of the batch.
	if len(candidate) > s.opts.MaxRecords || totalBytes > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	// Commit.
	if len(b.Ops) > 0 {
		s.generation++
	}
	s.records = candidate
	s.revision = revision
	s.totalBytes = totalBytes

	names := make([]string, 0, len(touched))
	for name := range touched {
		if _, ok := candidate[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	changed := make([]Record, 0, len(names))
	for _, name := range names {
		r := candidate[name]
		v := make([]byte, len(r.value))
		copy(v, r.value)
		changed = append(changed, Record{Name: name, Value: v, Revision: r.revision})
	}
	return Result{Generation: s.generation, Revision: s.revision, Changed: changed}, nil
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
	v := make([]byte, len(r.value))
	copy(v, r.value)
	return Record{Name: name, Value: v, Revision: r.revision}, true, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.records))
	for name := range s.records {
		names = append(names, name)
	}
	sort.Strings(names)
	recs := make([]Record, 0, len(names))
	for _, name := range names {
		r := s.records[name]
		v := make([]byte, len(r.value))
		copy(v, r.value)
		recs = append(recs, Record{Name: name, Value: v, Revision: r.revision})
	}
	return Snapshot{Generation: s.generation, NextRevision: s.revision + 1, Records: recs}
}
