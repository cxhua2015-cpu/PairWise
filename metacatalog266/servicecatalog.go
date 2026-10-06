package metacatalog266

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

func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	if len(b.Ops) == 0 {
		s.mu.RLock()
		r := Result{Generation: s.generation, Revision: s.nextRevision - 1}
		s.mu.RUnlock()
		return r, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	candidate := make(map[string]entry, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	nextRev := s.nextRevision
	touched := make(map[string]struct{})

	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			candidate[op.Name] = entry{value: v, revision: nextRev}
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

	total := 0
	for _, e := range candidate {
		total += len(e.value)
	}
	if len(candidate) > s.opts.MaxRecords || total > s.opts.MaxTotalValueBytes {
		return Result{}, ErrCapacity
	}

	s.records = candidate
	s.generation++
	s.nextRevision = nextRev

	names := make([]string, 0, len(touched))
	for n := range touched {
		if _, ok := candidate[n]; ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	changed := make([]Record, 0, len(names))
	for _, n := range names {
		e := candidate[n]
		v := make([]byte, len(e.value))
		copy(v, e.value)
		changed = append(changed, Record{Name: n, Value: v, Revision: e.revision})
	}
	return Result{Generation: s.generation, Revision: nextRev - 1, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := s.opts.validateName(name); err != nil {
		return Record{}, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.records[name]
	if !ok {
		return Record{}, false, nil
	}
	v := make([]byte, len(e.value))
	copy(v, e.value)
	return Record{Name: name, Value: v, Revision: e.revision}, true, nil
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
		v := make([]byte, len(e.value))
		copy(v, e.value)
		recs = append(recs, Record{Name: n, Value: v, Revision: e.revision})
	}
	return Snapshot{Generation: s.generation, NextRevision: s.nextRevision, Records: recs}
}
