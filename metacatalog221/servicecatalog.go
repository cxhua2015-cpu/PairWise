package metacatalog221

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

// Store is a concurrency-safe in-memory metadata catalog.
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

// Apply executes the batch atomically in input order. Put allocates
// consecutive revisions; Delete allocates none. Capacity limits on record
// count and total value bytes are checked only at the end of the batch.
// Any failure rolls back records, generation and revision.
func (s *Store) Apply(b Batch) (Result, error) {
	if err := s.ValidateBatch(b); err != nil {
		return Result{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	candidate := make(map[string]entry, len(s.records))
	for k, v := range s.records {
		candidate[k] = v
	}
	nextRev := s.nextRevision
	lastRev := s.nextRevision - 1
	for _, op := range b.Ops {
		switch op.Kind {
		case Put:
			v := make([]byte, len(op.Value))
			copy(v, op.Value)
			candidate[op.Name] = entry{value: v, revision: nextRev}
			lastRev = nextRev
			nextRev++
		case Delete:
			if _, ok := candidate[op.Name]; !ok {
				return Result{}, ErrNotFound
			}
			delete(candidate, op.Name)
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
	s.nextRevision = nextRev
	if len(b.Ops) > 0 {
		s.generation++
	}

	names := make([]string, 0, len(candidate))
	touched := make(map[string]bool, len(b.Ops))
	for _, op := range b.Ops {
		if e, ok := candidate[op.Name]; ok && !touched[op.Name] {
			touched[op.Name] = true
			names = append(names, op.Name)
			_ = e
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
	return Result{Generation: s.generation, Revision: lastRev, Changed: changed}, nil
}

func (s *Store) Get(name string) (Record, bool, error) {
	if err := validName(name, s.opts.MaxNameBytes); err != nil {
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
